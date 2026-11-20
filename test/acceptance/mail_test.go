// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

package acceptance

import (
	"net/http"
	"net/url"
	"strconv"
	"testing"

	"github.com/divbast/ci/harness"
)

// Mail through the lab's relay, which speaks SMTP over implicit TLS on
// 465 with a certificate from the test CA, and takes any credentials. Port
// 465 alone is what turns implicit TLS on: no TLS setting is given. The
// domain is the sender's, which every message's From needs.
const (
	// relayUser and relayLogin are the user and the password the blog
	// logs in to the relay with: the user from config.ini, the password
	// from its file.
	relayUser  = "lab"
	relayLogin = "lab-relay-pass"
	mailINI    = "[app]\nhost = " + blogBase + "\nsingle_user = true\n" +
		"[email]\ndomain = example.test\nsmtp_host = " + labIP + "\nsmtp_port = 465\nsmtp_username = " + relayUser + "\n"
	// mailRunDir is where the SMTP password's file, relay-login, is
	// mounted, and mailPasswordFile names it for
	// WRITEFREELY_SMTP_PASSWORD_FILE.
	mailRunDir       = "/run/smtp"
	mailPasswordFile = mailRunDir + "/relay-login"
	mailReader       = "reader@example.test"
)

// TestMailOverImplicitTLS subscribes a reader to the admin's blog by
// email, from the lab. The blog sends the confirmation through the lab's
// relay, logged in as the configured user with the password it read from
// the file WRITEFREELY_SMTP_PASSWORD_FILE names, without the file's
// newline, and it arrives over TLS.
func TestMailOverImplicitTLS(t *testing.T) {
	t.Parallel()
	e, image := underTest(t)
	network := labNetwork(t, e)
	_, lab := startLab(t, e, image, network, labDir(t))
	password := harness.Mount{
		Source:   hostDir(t, 0o755, map[string]string{"relay-login": relayLogin + "\n"}, 0o444),
		Target:   mailRunDir,
		ReadOnly: true,
	}
	startBlog(t, e, image, network, mailINI,
		map[string]string{adminUser: fedAlias, "WRITEFREELY_SMTP_PASSWORD_FILE": mailPasswordFile},
		secretMount(t, cliAdminPassword), password)

	subscribe := blogBase + "/api/collections/" + fedAlias + "/email/subscribe"
	resp, err := lab.HTTP(t.Context(), http.MethodPost, subscribe, nil, url.Values{"email": {mailReader}}.Encode())
	checks := harness.NewChecks(t)
	if err != nil {
		checks.True("subscribing "+mailReader+" answers 202", false, err.Error())
	} else {
		checks.True("subscribing "+mailReader+" answers 202", resp.Status == http.StatusAccepted,
			strconv.Itoa(resp.Status)+" "+harness.Clip(string(resp.Body)))
	}
	harness.RunAppRecords(t, lab, "mail-wait", "30", mailReader, relayUser, relayLogin)
}
