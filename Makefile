# Cross-compile the Go services for Linux and collect everything install.sh needs into dist/.
dist:
	rm -rf dist && mkdir -p dist/bin
	cd agent && for c in core-agent dbmanager backup notifier; do GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ../dist/bin/$$c ./cmd/$$c; done
	cd agent && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o ../dist/bin/mail-service ./cmd/mail
	cd panel && npm ci && npm run build
	rsync -a --delete --exclude node_modules --exclude .env --exclude 'storage/logs/*' --exclude 'bootstrap/cache/*.php' --exclude tests --exclude 'database/*.sqlite' panel/ dist/panel/
	cp -r deploy dist/deploy && cp deploy/install.sh dist/install.sh
.PHONY: dist
