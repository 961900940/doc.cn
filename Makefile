.PHONY: web-build server-run server-build dev

web-build:
	cd web && npm run build
	rm -rf server/public
	mkdir -p server/public
	cp -R web/dist/. server/public/

server-run:
	cd server && go run .

server-build:
	cd server && go build -o doc-system .

dev:
	@echo "Run backend: make server-run"
	@echo "Run frontend: cd web && npm run dev"
