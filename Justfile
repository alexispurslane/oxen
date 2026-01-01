default:
  @just --list

build:
  go build -o oxen main.go

build-linux:
  GOOS=linux GOARCH=amd64 go build -o oxen-linux-amd64 main.go

build-darwin:
  GOOS=darwin GOARCH=amd64 go build -o oxen-darwin-amd64 main.go

clean:
  rm -f oxen oxen-linux-amd64 oxen-darwin-amd64