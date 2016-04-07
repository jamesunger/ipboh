gx:
	go get -u github.com/whyrusleeping/gx
	go get -u github.com/whyrusleeping/gx-go

deps: gx
	./bin/gx --verbose install --global

build: deps
	go build ipboh.go

build-windows: deps
	GOOS=windows GOARCH=amd64 go build -tags nofuse ipboh.go

build-android: deps
	CGO_ENABLED=1 CC=arm-linux-androideabi-gcc CXX=arm-linux-androideabi-g++ GOOS=android GOARCH=arm GOARM=7 go build ipboh.go

build-pi: deps
	GOOS=linux GOARCH=arm GOARM=5 go build ipboh.go

test: deps
	rm -rf test/.ipfs
	rm -f test/ipboh-index.txt
	rm -f test/.ipbohrc
	echo "hi" | go test -v -cover -coverprofile cover.out

install: build
	sudo cp ipboh /usr/local/bin

