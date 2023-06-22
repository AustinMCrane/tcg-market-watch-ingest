install:
	go install
	go generate
	go build
build:
	go build
test:
	go test -cover
