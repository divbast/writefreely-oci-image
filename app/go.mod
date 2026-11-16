// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

module github.com/divbast/writefreely-oci-image/app

go 1.27.4

require (
	// config.Save's own serialiser, so the admin panel's saves write the
	// file as WriteFreely does, without the chmod that fails on a mounted
	// file the container does not own
	github.com/go-ini/ini v1.67.0
	// InitRoutes takes a *mux.Router
	github.com/gorilla/mux v1.8.1
	github.com/writefreely/writefreely v0.17.3
)

require (
	code.as/core/socks v1.0.0 // indirect
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/PuerkitoBio/goquery v1.8.1 // indirect
	github.com/andybalholm/cascadia v1.3.2 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/beevik/etree v1.1.0 // indirect
	github.com/captncraig/cors v0.0.0-20190703115713-e80254a89df1 // indirect
	github.com/chzyer/readline v0.0.0-20180603132655-2972be24d48e // indirect
	github.com/dchest/uniuri v0.0.0-20200228104902-7aecb25e1fe5 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/fatih/structs v1.1.0 // indirect
	github.com/go-fed/httpsig v0.1.1-0.20200204213531-0ef28562fabe // indirect
	github.com/go-sql-driver/mysql v1.8.1 // indirect
	github.com/gobuffalo/envy v1.9.0 // indirect
	github.com/gofrs/uuid v3.3.0+incompatible // indirect
	github.com/gologme/log v1.2.0 // indirect
	github.com/gorilla/csrf v1.7.3 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/gorilla/feeds v1.1.2 // indirect
	github.com/gorilla/schema v1.4.1 // indirect
	github.com/gorilla/securecookie v1.1.2 // indirect
	github.com/gorilla/sessions v1.3.0 // indirect
	github.com/gosimple/slug v1.15.0 // indirect
	github.com/gosimple/unidecode v1.0.1 // indirect
	github.com/guregu/null v4.0.0+incompatible // indirect
	github.com/hashicorp/errwrap v1.0.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/ikeikeikeike/go-sitemap-generator/v2 v2.0.2 // indirect
	github.com/joho/godotenv v1.3.0 // indirect
	github.com/kylemcc/twitter-text-go v0.0.0-20180726194232-7f582f6736ec // indirect
	github.com/mailgun/mailgun-go v2.0.0+incompatible // indirect
	github.com/manifoldco/promptui v0.9.0 // indirect
	github.com/mattn/go-colorable v0.1.14 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-sqlite3 v1.14.28 // indirect
	github.com/microcosm-cc/bluemonday v1.0.27 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/nu7hatch/gouuid v0.0.0-20131221200532-179d4d0c4d8d // indirect
	github.com/nxadm/tail v1.4.11 // indirect
	github.com/petermattis/goid v0.0.0-20180202154549-b0b1615b78e5 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/rainycape/unidecode v0.0.0-20150907023854-cb7f23ec59be // indirect
	github.com/rogpeppe/go-internal v1.9.0 // indirect
	github.com/sasha-s/go-deadlock v0.3.1 // indirect
	github.com/shurcooL/sanitized_anchor_name v1.0.0 // indirect
	github.com/toorop/go-dkim v0.0.0-20201103131630-e1cd1a0a5208 // indirect
	github.com/writeas/activity v0.1.2 // indirect
	github.com/writeas/activityserve v0.0.0-20230428180247-dc13a4f4d835 // indirect
	github.com/writeas/go-strip-markdown/v2 v2.1.1 // indirect
	github.com/writeas/go-webfinger v1.1.0 // indirect
	github.com/writeas/go-writeas/v2 v2.0.2 // indirect
	github.com/writeas/httpsig v1.0.0 // indirect
	github.com/writeas/impart v1.1.1 // indirect
	github.com/writeas/import v0.2.1 // indirect
	github.com/writeas/monday v1.3.0 // indirect
	github.com/writeas/openssl-go v1.0.0 // indirect
	github.com/writeas/saturday v1.7.2-0.20200427193424-392b95a03320 // indirect
	github.com/writeas/slug v1.2.0 // indirect
	github.com/writeas/web-core v1.8.0 // indirect
	github.com/writefreely/go-gopher v0.0.0-20220429181814-40127126f83b // indirect
	github.com/writefreely/go-nodeinfo v1.2.0 // indirect
	github.com/xhit/go-simple-mail/v2 v2.16.0 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	gopkg.in/ini.v1 v1.62.0 // indirect
)
