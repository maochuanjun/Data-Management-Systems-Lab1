.PHONY: run test integration vet build
run:
	go run ./cmd/server
test:
	go test ./...
integration:
	@test -n "$$INTEGRATION_MONGO_URI" || (echo '请设置 INTEGRATION_MONGO_URI'; exit 1)
	go test -count=1 -race ./...
vet:
	go vet ./...
build:
	go build -o bin/survey-lab ./cmd/server
