// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package main

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/divbast/ci/harness/probekit"
)

// signedHeaders are what the lab's signatures cover, in order: the
// draft-cavage HTTP signature that WriteFreely and Mastodon make and
// check, as rsa-sha256.
var signedHeaders = []string{"(request-target)", "host", "date", "digest"}

// asContext is the JSON-LD context of the lab's activities.
const asContext = "https://www.w3.org/ns/activitystreams"

// inboxMaxBytes is the most the blog's inbox takes by default: the
// oversize post is one byte past it, and more.
const inboxMaxBytes = 1 << 20

// digest is the Digest header of body.
func digest(body []byte) string {
	sum := sha256.Sum256(body)
	return "SHA-256=" + base64.StdEncoding.EncodeToString(sum[:])
}

// signingString is what a signature over headers signs, for a request
// with method, target, its path and query as the request line carries
// them, host and header.
func signingString(headers []string, method, target, host string, header http.Header) (string, error) {
	lines := make([]string, 0, len(headers))
	for _, name := range headers {
		name = strings.ToLower(name)
		var value string
		switch name {
		case "(request-target)":
			value = strings.ToLower(method) + " " + target
		case "host":
			value = host
		default:
			values := header.Values(name)
			if len(values) == 0 {
				return "", fmt.Errorf("the signature covers %s, which the request lacks", name)
			}
			value = strings.Join(values, ", ")
		}
		lines = append(lines, name+": "+value)
	}
	return strings.Join(lines, "\n"), nil
}

// sign signs req, with key under keyID, over signedHeaders, setting its
// Signature header. req's Date and Digest are set already.
func sign(req *http.Request, keyID string, key *rsa.PrivateKey) error {
	s, err := signingString(signedHeaders, req.Method, req.URL.RequestURI(), req.URL.Host, req.Header)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(s))
	sig, err := rsa.SignPKCS1v15(nil, key, crypto.SHA256, sum[:])
	if err != nil {
		return err
	}
	req.Header.Set("Signature", fmt.Sprintf(`keyId=%q,algorithm="rsa-sha256",headers=%q,signature=%q`,
		keyID, strings.Join(signedHeaders, " "), base64.StdEncoding.EncodeToString(sig)))
	return nil
}

// parseSignature returns a Signature header's parameters, each
// key="value", separated by commas.
func parseSignature(header string) (map[string]string, error) {
	params := make(map[string]string)
	rest := strings.TrimSpace(header)
	for rest != "" {
		key, after, ok := strings.Cut(rest, "=")
		end := strings.IndexByte(strings.TrimPrefix(after, `"`), '"')
		if !ok || !strings.HasPrefix(after, `"`) || end < 0 {
			return nil, fmt.Errorf("%s is not a Signature header", strconv.Quote(header))
		}
		params[strings.TrimSpace(key)] = after[1 : 1+end]
		rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(after[2+end:]), ","))
	}
	return params, nil
}

// verifySignature checks the signature on a request the lab's inbox
// took, against pub: rsa-sha256, over at least signedHeaders. It returns
// the signature's keyId and the headers it covers.
func verifySignature(e *inboxEntry, pub *rsa.PublicKey) (keyID, covered string, err error) {
	header := e.Header.Get("Signature")
	if header == "" {
		return "", "", errors.New("the request has no Signature header")
	}
	params, err := parseSignature(header)
	if err != nil {
		return "", "", err
	}
	keyID, covered = params["keyId"], params["headers"]
	if alg := params["algorithm"]; alg != "rsa-sha256" {
		return keyID, covered, fmt.Errorf("the signature's algorithm is %q, not rsa-sha256", alg)
	}
	names := strings.Fields(strings.ToLower(covered))
	for _, need := range signedHeaders {
		if !slices.Contains(names, need) {
			return keyID, covered, fmt.Errorf("the signature covers %q, without %s", covered, need)
		}
	}
	s, err := signingString(names, e.Method, e.Target, e.Host, e.Header)
	if err != nil {
		return keyID, covered, err
	}
	sig, err := base64.StdEncoding.DecodeString(params["signature"])
	if err != nil {
		return keyID, covered, fmt.Errorf("the signature is not base64: %w", err)
	}
	sum := sha256.Sum256([]byte(s))
	if err := rsa.VerifyPKCS1v15(pub, crypto.SHA256, sum[:], sig); err != nil {
		return keyID, covered, fmt.Errorf("rsa-sha256 over %q: %w", covered, err)
	}
	return keyID, covered, nil
}

// loadRSAKey reads the RSA private key in the PEM file at path, PKCS #8
// or PKCS #1.
func loadRSAKey(path string) (*rsa.PrivateKey, error) {
	text, err := readFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(text)
	if block == nil {
		return nil, fmt.Errorf("%s holds no PEM block", path)
	}
	if key, pkcs1Err := x509.ParsePKCS1PrivateKey(block.Bytes); pkcs1Err == nil {
		return key, nil
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s holds a %T, not an RSA key", path, key)
	}
	return rsaKey, nil
}

// labKey is the lab actor's key, from the lab's directory.
func labKey() (*rsa.PrivateKey, error) {
	return loadRSAKey(labMount + "/actor.pem")
}

// activityID returns a fresh id for an activity of the lab's.
func activityID() string {
	return labBase + "/activities/" + strings.ToLower(rand.Text())
}

// postActivity posts body to inbox as ActivityPub, with Date and Digest,
// and signs it with key under keyID unless key is nil. It returns the
// status and the answer.
func postActivity(ctx context.Context, inbox string, body []byte, keyID string, key *rsa.PrivateKey) (status int, answer []byte, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, inbox, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", activityJSON)
	req.Header.Set("Accept", activityJSON)
	req.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))
	req.Header.Set("Digest", digest(body))
	if key != nil {
		if err := sign(req, keyID, key); err != nil {
			return 0, nil, err
		}
	}
	return do(req)
}

// activity is one activity of the lab's, as JSON.
func activity(typ, actor string, object any) ([]byte, error) {
	return json.Marshal(map[string]any{
		"@context": asContext,
		"id":       activityID(),
		"type":     typ,
		"actor":    actor,
		"object":   object,
	})
}

// follow sends a Follow of the blog's actor from the lab's, signed with
// the lab's key, to inbox, and records whether it is accepted: any 2xx.
func follow(ctx context.Context, out *probekit.Output, args []string) error {
	inbox, object := args[0], args[1]
	key, err := labKey()
	if err != nil {
		return err
	}
	body, err := activity("Follow", labActor, object)
	if err != nil {
		return err
	}
	what := "a signed Follow of " + object + " is accepted"
	status, answer, err := postActivity(ctx, inbox, body, labKeyID, key)
	if err != nil {
		out.Record(false, what, "2xx", err.Error())
		return nil
	}
	out.Record(status/100 == 2, what, "2xx", fmt.Sprintf("%d %s", status, clip(answer)))
	return nil
}

// inboxPosts are inbox-post's modes: each builds a post the blog's inbox
// must answer as the suite expects, and returns its body, keyId and key,
// nil for none.
var inboxPosts = map[string]func(object string, lab *rsa.PrivateKey) (body []byte, keyID string, key *rsa.PrivateKey, err error){
	// A Follow with no signature at all
	"unsigned": func(object string, _ *rsa.PrivateKey) ([]byte, string, *rsa.PrivateKey, error) {
		body, err := activity("Follow", labActor, object)
		return body, "", nil, err
	},
	// A Follow under the lab's keyId, signed with another key
	"wrong-key": func(object string, _ *rsa.PrivateKey) ([]byte, string, *rsa.PrivateKey, error) {
		other, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			return nil, "", nil, err
		}
		body, err := activity("Follow", labActor, object)
		return body, labKeyID, other, err
	},
	// A Follow signed by the lab, naming another actor as its own
	"wrong-actor": func(object string, lab *rsa.PrivateKey) ([]byte, string, *rsa.PrivateKey, error) {
		body, err := activity("Follow", labBase+"/users/other", object)
		return body, labKeyID, lab, err
	},
	// A Create signed by the lab, past the inbox's size limit
	"oversize": func(_ string, lab *rsa.PrivateKey) ([]byte, string, *rsa.PrivateKey, error) {
		body, err := activity("Create", labActor, map[string]string{
			"type":    "Note",
			"content": strings.Repeat("a", inboxMaxBytes+1),
		})
		return body, labKeyID, lab, err
	},
	// A Delete of an account whose actor, and key, answer 410
	"gone-delete": func(_ string, lab *rsa.PrivateKey) ([]byte, string, *rsa.PrivateKey, error) {
		body, err := activity("Delete", goneActor, goneActor)
		return body, goneActor + "#main-key", lab, err
	},
}

// inboxPost sends inbox the post mode names, and records whether its
// status is want. The object of each Follow is the actor whose inbox it
// is.
func inboxPost(ctx context.Context, out *probekit.Output, args []string) error {
	inbox, mode, want := args[0], args[1], args[2]
	build, ok := inboxPosts[mode]
	if !ok {
		modes := make([]string, 0, len(inboxPosts))
		for m := range inboxPosts {
			modes = append(modes, m)
		}
		slices.Sort(modes)
		return fmt.Errorf("the mode is '%s', and must be one of %s", mode, strings.Join(modes, ", "))
	}
	if err := statusWanted(want); err != nil {
		return err
	}
	lab, err := labKey()
	if err != nil {
		return err
	}
	body, keyID, key, err := build(strings.TrimSuffix(inbox, "/inbox"), lab)
	if err != nil {
		return err
	}
	what := "inbox-post " + mode + " to " + inbox + " answers " + want
	status, answer, err := postActivity(ctx, inbox, body, keyID, key)
	if err != nil {
		out.Record(false, what, want, err.Error())
		return nil
	}
	found := strconv.Itoa(status)
	out.Record(want == anyStatus || found == want, what, want, found+" "+clip(answer))
	return nil
}

// activityType returns the type of the activity in body, empty when it
// names none.
func activityType(body string) string {
	var a struct {
		Type string `json:"type"`
	}
	if json.Unmarshal([]byte(body), &a) != nil {
		return ""
	}
	return a.Type
}

// inboxWait waits up to seconds for an activity of typ in the lab's
// inbox, then fetches the blog's actor and checks the activity's Digest,
// its keyId and its HTTP signature against the actor's publicKeyPem.
func inboxWait(ctx context.Context, out *probekit.Output, args []string) error {
	typ, actorURL := args[0], args[2]
	within, err := parseSeconds(args[1])
	if err != nil {
		return err
	}
	arrives := "an activity of type " + typ + " arrives in the lab's inbox within " + args[1] + " s"
	e, seen, err := waitJournal(ctx, inboxJournal, within, func(e *inboxEntry) bool { return activityType(e.Body) == typ })
	if err != nil {
		out.Record(false, arrives, typ, fmt.Sprintf("%v; the inbox took %d others", err, seen))
		return nil
	}
	out.Record(true, arrives, typ, e.Method+" "+e.Target+" from "+e.Remote)

	gotDigest, wantDigest := e.Header.Get("Digest"), digest([]byte(e.Body))
	out.Record(gotDigest == wantDigest, "the "+typ+"'s Digest matches its body", wantDigest, gotDigest)

	verifies := "signature verifies"
	expected := "rsa-sha256 by " + actorURL + "'s publicKeyPem"
	actor, err := fetchActor(ctx, actorURL)
	if err != nil {
		out.Record(false, verifies, expected, err.Error())
		return nil
	}
	pub, err := parseRSAPublicKey(actor.PublicKey.PublicKeyPem)
	if err != nil {
		out.Record(false, verifies, expected, err.Error())
		return nil
	}
	keyID, covered, err := verifySignature(e, pub)
	out.Equal("the "+typ+" is signed with "+actorURL+"'s key", actor.PublicKey.ID, keyID)
	if err != nil {
		out.Record(false, verifies, expected, err.Error())
		return nil
	}
	out.Record(true, verifies, expected, "keyId "+keyID+", over "+covered)
	return nil
}
