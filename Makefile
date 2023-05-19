VERSION=v0.0.1

bin: bin/mf_darwin bin/mf_linux 

bin/mf_darwin:
	mkdir -p bin
	GOOS=darwin GOARCH=amd64 go build -ldflags="-X 'main.Version=$(VERSION)'" -o bin/mf_darwin cmd/mf/*.go
	openssl sha512 bin/mf_darwin > bin/mf_darwin.sha512

bin/mf_linux:
	mkdir -p bin
	GOOS=linux GOARCH=amd64 go build -ldflags="-X 'main.Version=$(VERSION)'" -o bin/mf_linux cmd/mf/*.go
	openssl sha512 bin/mf_linux > bin/mf_linux.sha512
