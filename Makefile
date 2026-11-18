# Cross-compile the Go services for Linux and collect everything install.sh needs into dist/.
# Optional: path to a CadWAF firewall checkout to bundle as an offline fallback (the panel normally downloads releases from GitHub).
FIREWALL_SRC ?=

dist:
	rm -rf dist && mkdir -p dist/bin
	cd agent && for c in core-agent dbmanager backup notifier; do GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ../dist/bin/$$c ./cmd/$$c; done
	cd agent && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ../dist/bin/mail-service ./cmd/mail
	cd agent && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ../dist/bin/system-service ./cmd/system
	# CadWAF eBPF firewall (optional): built from FIREWALL_SRC, the pre-compiled BPF object is embedded in the binaries.
	@if [ -n "$(FIREWALL_SRC)" ] && [ -d "$(FIREWALL_SRC)" ]; then for a in amd64 arm64; do mkdir -p dist/firewall/$$a; \
		(cd "$(FIREWALL_SRC)" && for c in firewalld firewallctl; do CGO_ENABLED=0 GOOS=linux GOARCH=$$a go build -buildvcs=false -o "$(CURDIR)/dist/firewall/$$a/$$c" ./cmd/$$c || exit 1; done) || exit 1; done; \
		else echo "FIREWALL_SRC not set: no offline CadWAF copy is bundled (it is downloaded from GitHub when installed)"; fi
	cd panel && npm ci && npm run build
	rsync -a --delete --exclude node_modules --exclude .env --exclude 'storage/logs/*' --exclude 'bootstrap/cache/*.php' --exclude tests --exclude 'database/*.sqlite' panel/ dist/panel/
	cp -r deploy dist/deploy && cp deploy/install.sh dist/install.sh
.PHONY: dist
