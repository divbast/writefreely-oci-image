// SPDX-License-Identifier: AGPL-3.0-only
// SPDX-FileCopyrightText: 2026 Aryan Ameri <github@aryan.ameri.coffee>

module github.com/divbast/writefreely-oci-image/app

go 1.27.4

// WriteFreely is built from divbast's fork, which carries fixes the image
// relies on, as builds tagged v0.17.3-divbast.N: upstream's development
// branch after the 0.17.3 release, plus fixes. The require below stays at
// that release, and the module path, so every import, stays upstream's.
// The replace goes once an upstream release has the fixes, and with it the
// Containerfile's two checks that the build takes nothing from
// WriteFreely's own repository. Renovate moves the fork build here and the
// Containerfile's WRITEFREELY_FORK_VERSION in one PR, and the build fails
// unless they match.
replace github.com/writefreely/writefreely => github.com/aryonoco/writefreely v0.17.3-divbast.27

require (
	// config.Save's own serialiser, so the admin panel's saves write the
	// file as WriteFreely does, without the chmod that fails on a mounted
	// file the container does not own; and config.Load's own parser, so
	// the health probe reads the two keys it needs as WriteFreely does,
	// without config.Load reading every secret's _FILE as well
	github.com/go-ini/ini v1.67.0
	// InitRoutes takes a *mux.Router
	github.com/gorilla/mux v1.8.1
	// user create asks for the password at the terminal, masked, through
	// the same prompt WriteFreely's own user reset-pass uses
	github.com/manifoldco/promptui v0.9.0
	// Whether stdin is a terminal decides between that prompt and one
	// line of stdin; WriteFreely already links it, through fatih/color
	github.com/mattn/go-isatty v0.0.24
	github.com/writefreely/writefreely v0.17.3
)

require (
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/PuerkitoBio/goquery v1.14.0 // indirect
	github.com/andybalholm/cascadia v1.3.5 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/beevik/etree v1.8.1 // indirect
	github.com/captncraig/cors v0.0.0-20190703115713-e80254a89df1 // indirect
	github.com/chzyer/readline v1.5.1 // indirect
	github.com/dustin/go-humanize v1.1.0 // indirect
	github.com/fatih/color v1.19.0 // indirect
	github.com/fatih/structs v1.1.0 // indirect
	github.com/go-sql-driver/mysql v1.10.2 // indirect
	github.com/gobuffalo/envy v1.10.2 // indirect
	github.com/gofrs/uuid v4.4.0+incompatible // indirect
	github.com/gorilla/csrf v1.7.3 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/gorilla/feeds v1.2.0 // indirect
	github.com/gorilla/schema v1.4.1 // indirect
	github.com/gorilla/securecookie v1.1.2 // indirect
	github.com/gorilla/sessions v1.4.0 // indirect
	github.com/gosimple/slug v1.15.0 // indirect
	github.com/gosimple/unidecode v1.0.1 // indirect
	github.com/guregu/null v4.0.0+incompatible // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/ikeikeikeike/go-sitemap-generator/v2 v2.0.2 // indirect
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/kylemcc/twitter-text-go v0.0.0-20180726194232-7f582f6736ec // indirect
	github.com/mailgun/mailgun-go v2.0.0+incompatible // indirect
	github.com/mattn/go-colorable v0.1.16 // indirect
	github.com/mattn/go-sqlite3 v1.14.53 // indirect
	github.com/microcosm-cc/bluemonday v1.0.27 // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/nu7hatch/gouuid v0.0.0-20131221200532-179d4d0c4d8d // indirect
	github.com/nxadm/tail v1.4.11 // indirect
	github.com/petermattis/goid v0.0.0-20261124144452-466dc414edf5 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/rogpeppe/go-internal v1.16.0 // indirect
	github.com/sasha-s/go-deadlock v0.3.9 // indirect
	github.com/toorop/go-dkim v0.0.0-20250226130143-9025cce95817 // indirect
	github.com/writeas/activity v0.1.2 // indirect
	github.com/writeas/go-strip-markdown/v2 v2.1.1 // indirect
	github.com/writeas/go-webfinger v1.1.0 // indirect
	github.com/writeas/go-writeas/v2 v2.1.1 // indirect
	github.com/writeas/httpsig v1.0.0 // indirect
	github.com/writeas/impart v1.1.1 // indirect
	github.com/writeas/import v0.2.1 // indirect
	github.com/writeas/monday v1.3.0 // indirect
	github.com/writeas/openssl-go v1.0.0 // indirect
	github.com/writeas/web-core v1.8.0 // indirect
	github.com/writefreely/go-gopher v0.0.0-20260201132918-6d40a5df699c // indirect
	github.com/writefreely/go-nodeinfo v1.2.0 // indirect
	github.com/xhit/go-simple-mail/v2 v2.17.0 // indirect
	github.com/yuin/goldmark/v2 v2.1.16 // indirect
	golang.org/x/crypto v0.59.0 // indirect
	golang.org/x/mod v0.43.0 // indirect
	golang.org/x/net v0.63.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	h12.io/socks v1.0.3 // indirect
)
