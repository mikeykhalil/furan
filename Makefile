.PHONY: furan release

furan:
	go install github.com/dollarshaveclub/furan

release:
	rm -rf build releases
	mkdir -p releases build/furan-linux-amd64 build/furan-darwin-amd64

	GOOS=linux GOARCH=amd64 go build -o build/furan-linux-amd64/furan \
		github.com/dollarshaveclub/furan
	GOOS=darwin GOARCH=amd64 go build -o build/furan-darwin-amd64/furan \
		github.com/dollarshaveclub/furan

	tar -C build -c furan-linux-amd64 | gzip -c > releases/furan-linux-amd64.tar.gz
	tar -C build -c furan-darwin-amd64 | gzip -c > releases/furan-darwin-amd64.tar.gz
