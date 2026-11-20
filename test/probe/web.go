// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package main

import (
	"bytes"
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/divbast/ci/harness/probekit"
)

// activityJSON is ActivityPub's media type.
const activityJSON = "application/activity+json"

// httpTimeout bounds one request the probe sends, its answer and all.
const httpTimeout = 30 * time.Second

// maxBody is as much of an answer as the probe reads.
const maxBody = 4 << 20

// anyStatus is the expected status that takes whatever comes back.
const anyStatus = "any"

// client sends the probe's requests. It never follows a redirect, since
// the status is what a record asserts, and it trusts what SSL_CERT_FILE
// names: in the lab and the blog, the lab's test CA alone.
var client = &http.Client{
	Timeout:       httpTimeout,
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// send sends one request, with header and body, and returns the status
// and as much of the body as maxBody allows. The error is for a request
// that got no answer.
func send(ctx context.Context, method, url string, header http.Header, body []byte) (status int, answer []byte, err error) {
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	maps.Copy(req.Header, header)
	return do(req)
}

// do sends req, as send does.
func do(req *http.Request) (status int, answer []byte, err error) {
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	answer, err = io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return resp.StatusCode, nil, fmt.Errorf("reading the answer, status %d: %w", resp.StatusCode, err)
	}
	return resp.StatusCode, answer, nil
}

// statusWanted checks an expected status argument: a status code, or
// anyStatus.
func statusWanted(want string) error {
	if want == anyStatus {
		return nil
	}
	if code, err := strconv.Atoi(want); err != nil || code < 100 || code > 599 {
		return fmt.Errorf("the expected status is '%s', and must be a status code or %s", want, anyStatus)
	}
	return nil
}

// get records whether GET url answers want: that status code, or any
// answer at all when want is "any".
func get(ctx context.Context, out *probekit.Output, args []string) error {
	url, want := args[0], args[1]
	if err := statusWanted(want); err != nil {
		return err
	}
	what := "GET " + url + " answers " + want
	status, _, err := send(ctx, http.MethodGet, url, nil, nil)
	if err != nil {
		out.Record(false, what, want, err.Error())
		return nil
	}
	found := strconv.Itoa(status)
	out.Record(want == anyStatus || found == want, what, want, found)
	return nil
}

// actorDoc is the part of an ActivityPub actor the probe reads.
type actorDoc struct {
	ID        string `json:"id"`
	PublicKey struct {
		ID           string `json:"id"`
		Owner        string `json:"owner"`
		PublicKeyPem string `json:"publicKeyPem"`
	} `json:"publicKey"`
}

// fetchActor fetches the actor at url as ActivityPub, and returns it
// once it answers 200 with JSON.
func fetchActor(ctx context.Context, url string) (actorDoc, error) {
	status, body, err := send(ctx, http.MethodGet, url, http.Header{"Accept": {activityJSON}}, nil)
	if err != nil {
		return actorDoc{}, err
	}
	if status != http.StatusOK {
		return actorDoc{}, fmt.Errorf("GET %s as ActivityPub answered %d: %s", url, status, clip(body))
	}
	var actor actorDoc
	if err := json.Unmarshal(body, &actor); err != nil {
		return actorDoc{}, fmt.Errorf("GET %s as ActivityPub answered what is not an actor: %w: %s", url, err, clip(body))
	}
	return actor, nil
}

// parseRSAPublicKey returns the RSA key in text, a PEM block as an
// actor's publicKeyPem carries it: PKIX, as WriteFreely and Mastodon
// write it, or PKCS #1.
func parseRSAPublicKey(text string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(text))
	if block == nil {
		return nil, fmt.Errorf("no PEM block in %s", clip([]byte(text)))
	}
	switch block.Type {
	case "PUBLIC KEY":
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		pub, ok := key.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("the key is a %T, not an RSA key", key)
		}
		return pub, nil
	case "RSA PUBLIC KEY":
		return x509.ParsePKCS1PublicKey(block.Bytes)
	default:
		return nil, fmt.Errorf("the PEM block is a %s, not a public key", block.Type)
	}
}

// actorKey records whether the actor at url, fetched as ActivityPub,
// publishes a publicKey.publicKeyPem that parses as an RSA key.
func actorKey(ctx context.Context, out *probekit.Output, args []string) error {
	url := args[0]
	what := url + "'s publicKey.publicKeyPem parses as an RSA key"
	actor, err := fetchActor(ctx, url)
	if err != nil {
		out.Record(false, what, "an RSA public key", err.Error())
		return nil
	}
	pub, err := parseRSAPublicKey(actor.PublicKey.PublicKeyPem)
	if err != nil {
		out.Record(false, what, "an RSA public key", err.Error())
		return nil
	}
	out.Record(true, what, "an RSA public key", fmt.Sprintf("RSA, %d bits, key %s", pub.N.BitLen(), actor.PublicKey.ID))
	return nil
}

// publish logs user in through WriteFreely's API, publishes the body in
// bodyFile to the blog alias with the access token, and records the
// post's slug, which is the record's found.
func publish(ctx context.Context, out *probekit.Output, args []string) error {
	base, user, pass, alias, bodyFile := strings.TrimSuffix(args[0], "/"), args[1], args[2], args[3], args[4]
	text, err := readFile(bodyFile)
	if err != nil {
		return err
	}
	token, ok := recordLogin(ctx, out, base, user, pass)
	if !ok {
		return nil
	}

	posted, err := json.Marshal(map[string]string{"body": string(text)})
	if err != nil {
		return err
	}
	header := http.Header{"Content-Type": {"application/json"}, "Authorization": {"Token " + token}}
	status, answer, err := send(ctx, http.MethodPost, base+"/api/collections/"+alias+"/posts", header, posted)
	what := "publishing to " + alias + " gives the post's slug"
	switch {
	case err != nil:
		out.Record(false, what, "a slug", err.Error())
		return nil
	case status != http.StatusCreated:
		out.Record(false, what, "a slug", fmt.Sprintf("publishing to %s answered %d: %s", alias, status, clip(answer)))
		return nil
	}
	var post struct {
		Data struct {
			Slug string `json:"slug"`
		} `json:"data"`
	}
	if err := json.Unmarshal(answer, &post); err != nil {
		out.Record(false, what, "a slug", fmt.Sprintf("the answer is not a post: %v: %s", err, clip(answer)))
		return nil
	}
	out.Record(post.Data.Slug != "", what, "a slug", post.Data.Slug)
	return nil
}

// privateVisibility is a private blog's visibility in WriteFreely's
// collection API, whose JSON names the field "public".
const privateVisibility = 2

// blogPrivate logs user in through WriteFreely's API, and makes the blog
// alias private with the access token, as a JSON update: the collection
// API takes no other form without a session.
func blogPrivate(ctx context.Context, out *probekit.Output, args []string) error {
	base, user, pass, alias := strings.TrimSuffix(args[0], "/"), args[1], args[2], args[3]
	token, ok := recordLogin(ctx, out, base, user, pass)
	if !ok {
		return nil
	}
	update, err := json.Marshal(map[string]int{"public": privateVisibility})
	if err != nil {
		return err
	}
	header := http.Header{"Content-Type": {"application/json"}, "Authorization": {"Token " + token}}
	status, answer, err := send(ctx, http.MethodPost, base+"/api/collections/"+alias, header, update)
	what := "making " + alias + " private through the API answers 200"
	if err != nil {
		out.Record(false, what, "200", err.Error())
		return nil
	}
	out.Record(status == http.StatusOK, what, "200", fmt.Sprintf("%d %s", status, clip(answer)))
	return nil
}

// recordLogin logs user in through the API, records whether that gave an
// access token, and returns the token once it did.
func recordLogin(ctx context.Context, out *probekit.Output, base, user, pass string) (token string, ok bool) {
	token, err := apiLogin(ctx, base, user, pass)
	what := "logging in as " + user + " through the API gives an access token"
	if err != nil {
		out.Record(false, what, "an access token", err.Error())
		return "", false
	}
	out.Record(true, what, "an access token", "an access token")
	return token, true
}

// apiLogin logs user in through POST /api/auth/login and returns the
// access token the answer holds.
func apiLogin(ctx context.Context, base, user, pass string) (string, error) {
	creds, err := json.Marshal(map[string]string{"alias": user, "pass": pass})
	if err != nil {
		return "", err
	}
	status, answer, err := send(ctx, http.MethodPost, base+"/api/auth/login", http.Header{"Content-Type": {"application/json"}}, creds)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("POST /api/auth/login answered %d: %s", status, clip(answer))
	}
	var login struct {
		Data struct {
			AccessToken string `json:"access_token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(answer, &login); err != nil {
		return "", fmt.Errorf("POST /api/auth/login answered what is not a login: %w: %s", err, clip(answer))
	}
	if login.Data.AccessToken == "" {
		return "", errors.New("POST /api/auth/login answered no access token")
	}
	return login.Data.AccessToken, nil
}

// clipAt is as much of a text as clip quotes.
const clipAt = 300

// clip quotes b for a record's found: the whole of it up to clipAt
// bytes, and past that its first clipAt, then how many bytes there were.
func clip(b []byte) string {
	if len(b) <= clipAt {
		return strconv.Quote(string(b))
	}
	return fmt.Sprintf("%q... (%d bytes)", b[:clipAt], len(b))
}
