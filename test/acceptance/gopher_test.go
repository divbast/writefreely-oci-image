// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/divbast/ci/harness"
)

// Gopher on the lab network. The blog serves it on 7070, as gopherINI
// does, and the lab fetches each answer whole and compares it byte for
// byte with what is expected.
const (
	gopherAddr = blogIP + ":7070"
	// gopherLinksINI is a single-user blog whose Gopher links advertise
	// port 70, where a proxy in front would take Gopher, and the host of
	// [app] host: a name, not the address the lab reaches the blog at,
	// which nothing needs to resolve, since the lab sends to blogBase.
	gopherLinksINI = "[app]\nhost = http://blog.example.test:8080\nsingle_user = true\n[server]\ngopher_port = 7070\ngopher_advertised_port = 70\n"
	// gopherPostBody holds a line that is a lone dot, which would end a
	// Gopher text file early, and one that starts with a dot.
	gopherPostBody = "a\n.\n.b"
)

// The multi-user blog whose second user's blog goes private: every new
// blog is public, so it is in the root menu until then.
const (
	gopherSiteName = "divbast gopher"
	gopherHideINI  = "[app]\nhost = " + blogBase + "\nsingle_user = false\nsite_name = " + gopherSiteName +
		"\ndefault_visibility = public\n[server]\ngopher_port = 7070\n"
	// gopherOther is the second user, whose blog has their name.
	gopherOther = "grace"
)

// gopherError is the error item WriteFreely answers a selector it does
// not serve with, then the menu's end.
const gopherError = "3the page failed for some reason (see logs)\t\terror.host\t1\r\n.\r\n"

// gopherMenu is a menu with the info line title, then a directory item
// for each blog in aliases, at the blog's 7070, then the menu's end.
func gopherMenu(title string, aliases ...string) string {
	var b strings.Builder
	b.WriteString("i" + title + "\t\terror.host\t1\r\n")
	for _, alias := range aliases {
		b.WriteString("1" + alias + "\t/" + alias + "/\t" + blogIP + "\t7070\r\n")
	}
	b.WriteString(".\r\n")
	return b.String()
}

// labFile writes body into the lab's directory dir as name, readable by
// the lab's user, and returns the path the lab reads it at.
func labFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	if err := root.WriteFile(name, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// WriteFile's mode passes through the umask
	if err := root.Chmod(name, 0o644); err != nil {
		t.Fatal(err)
	}
	return labMount + "/" + name
}

// gopherAnswer is the answer testdata/name expects, as the server sends
// it. The file ends each line in LF, for its reader's sake, and holds
// {slug}, {date} and {long-date} where the post's slug and the day it was
// created go, as the menu and the post write them; the answer ends each
// line in CRLF.
func gopherAnswer(t *testing.T, name, slug string, created time.Time) string {
	t.Helper()
	b := hostFile(t, "testdata", name)
	fill := strings.NewReplacer("{slug}", slug, "{date}", created.Format(time.DateOnly), "{long-date}", created.Format("January 2, 2006"))
	return strings.ReplaceAll(fill.Replace(string(b)), "\n", "\r\n")
}

// publishPost publishes the post in the lab's file body to the blog alias
// as user, through the lab's publish, and returns its slug.
func publishPost(t *testing.T, lab *harness.Probe, user, pass, alias, body string) string {
	t.Helper()
	records, err := lab.RunApp(t.Context(), "publish", blogBase, user, pass, alias, body)
	if err != nil {
		t.Fatal(err)
	}
	harness.NewChecks(t).Records(records)
	if t.Failed() || len(records) != 2 {
		t.FailNow()
	}
	return records[1].Found
}

// postCreated asks the blog's API, from the lab, when the post at slug in
// the blog alias was created.
func postCreated(t *testing.T, lab *harness.Probe, alias, slug string) time.Time {
	t.Helper()
	url := blogBase + "/api/collections/" + alias + "/posts/" + slug
	resp, err := lab.HTTP(t.Context(), http.MethodGet, url, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	var post struct {
		Data struct {
			Created time.Time `json:"created"`
		} `json:"data"`
	}
	if resp.Status != http.StatusOK || json.Unmarshal(resp.Body, &post) != nil || post.Data.Created.IsZero() {
		t.Fatalf("GET %s answered %d, with no post's creation time: %s", url, resp.Status, harness.Clip(string(resp.Body)))
	}
	return post.Data.Created
}

// gopherListedTries is how many times, a second apart, the root menu is
// asked for until it lists a new post.
const gopherListedTries = 5

// TestGopherLinksAndFraming publishes a post with lines Gopher must
// escape. The root menu lists it with a link to the advertised port on
// the host of [app] host, and the post's selector answers its text with
// every line ended in CRLF, each line that starts with a dot given
// another, and the lone dot that ends the file.
func TestGopherLinksAndFraming(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	ctx := t.Context()
	network := labNetwork(t, e)
	dir := labDir(t)
	body := labFile(t, dir, "gopher-post.md", gopherPostBody)
	_, lab := startLab(t, e, image, network, dir)
	startBlog(t, e, image, network, gopherLinksINI, map[string]string{adminUser: fedAlias}, secretMount(t, cliAdminPassword))

	slug := publishPost(t, lab, fedAlias, cliAdminPassword, fedAlias, body)
	created := postCreated(t, lab, fedAlias, slug)
	// Written once the post's slug and date are known: the lab's
	// directory is a bind mount, so the lab reads them at once
	root := labFile(t, dir, "gopher-root.txt", gopherAnswer(t, "gopher-root.txt", slug, created))
	post := labFile(t, dir, "gopher-post.txt", gopherAnswer(t, "gopher-post.txt", slug, created))

	// WriteFreely lists a post once the second it was created in has
	// passed: until then, SQLite's text of its creation time, with the
	// fraction of the second, sorts after the time now. So the root menu
	// is asked for again, once a second, until it is as expected or the
	// tries run out; then it is checked, showing how it differs
	for range gopherListedTries {
		records, err := lab.RunApp(ctx, "gopher", gopherAddr, "/", root)
		if err == nil && len(records) == 1 && records[0].OK {
			break
		}
		if err := harness.Pause(ctx, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	harness.RunAppRecords(t, lab, "gopher", gopherAddr, "/", root)
	harness.RunAppRecords(t, lab, "gopher", gopherAddr, "/"+slug, post)
}

// TestGopherHidesPrivateBlog has a second user's blog go private through
// the API, on a multi-user blog. Until then, the root menu lists it, and
// its selector answers its menu; after, the root menu lists only the
// admin's blog, and its selector answers the same error item as a blog
// that does not exist.
func TestGopherHidesPrivateBlog(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	ctx := t.Context()
	network := labNetwork(t, e)
	dir := labDir(t)
	missing := labFile(t, dir, "gopher-error.txt", gopherError)
	both := labFile(t, dir, "gopher-root-both.txt", gopherMenu("Welcome to "+gopherSiteName, fedAlias, gopherOther))
	adminOnly := labFile(t, dir, "gopher-root-admin.txt", gopherMenu("Welcome to "+gopherSiteName, fedAlias))
	otherMenu := labFile(t, dir, "gopher-other.txt", gopherMenu(gopherOther))
	_, lab := startLab(t, e, image, network, dir)
	blog, _ := startBlog(t, e, image, network, gopherHideINI, map[string]string{adminUser: fedAlias}, secretMount(t, cliAdminPassword))

	res, err := blog.Exec(ctx, strings.NewReader(cliUserPassword+"\n"), binary, "user", "create", gopherOther)
	harness.NewChecks(t).True("user create "+gopherOther+" exits 0", err == nil && res.Code == 0, string(res.Stderr))
	harness.RunAppRecords(t, lab, "gopher", gopherAddr, "/", both)
	harness.RunAppRecords(t, lab, "gopher", gopherAddr, "/"+gopherOther+"/", otherMenu)

	harness.RunAppRecords(t, lab, "blog-private", blogBase, gopherOther, cliUserPassword, gopherOther)
	harness.RunAppRecords(t, lab, "gopher", gopherAddr, "/", adminOnly)
	harness.RunAppRecords(t, lab, "gopher", gopherAddr, "/"+gopherOther+"/", missing)
	harness.RunAppRecords(t, lab, "gopher", gopherAddr, "/nobody/", missing)
}

// gopherStopsWithin is how long a blog that cannot bind its Gopher port
// may take to stop.
const gopherStopsWithin = 10 * time.Second

// TestGopherBindFailureStops serves Gopher on 7070 in a container whose
// network namespace lets no port below 8000 be bound without a
// capability, which the contract drops, so 7070 cannot be bound. Gopher
// binds before the web server does: the process stops with exit status
// 1, and its log names the port.
func TestGopherBindFailureStops(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	ctx := t.Context()
	network := labNetwork(t, e)
	c, err := e.Start(ctx, t, &harness.RunSpec{
		Image:   image,
		Network: network,
		IP:      blogIP,
		Confine: harness.Contract(tmpfsSize),
		Env:     map[string]string{adminUser: fedAlias},
		Mounts:  []harness.Mount{dataVolume(t, e), configMount(t, gopherLinksINI, false), secretMount(t, cliAdminPassword)},
		Extra:   []string{"--sysctl", "net.ipv4.ip_unprivileged_port_start=8000"},
	})
	if err != nil {
		t.Fatalf("starting %s on the lab network: %v", image, err)
	}
	exitsOne(t, c, "Gopher's port 7070 cannot be bound", gopherStopsWithin, "port 7070")
}
