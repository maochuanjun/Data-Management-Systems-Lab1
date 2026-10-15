.PHONY: run test unit-test frontend-test integration e2e verify vet build
run:
	python3 scripts/with-local-mongo.py go run ./cmd/server
test:
	go test -count=1 ./tests/unit
	python3 -m unittest discover -s tests/unit -p '*_test.py'
	node --test tests/frontend/frontend.test.cjs
unit-test:
	go test -count=1 ./tests/unit
	python3 -m unittest discover -s tests/unit -p '*_test.py'
frontend-test:
	node --test tests/frontend/frontend.test.cjs
integration:
	@test -n "$$INTEGRATION_MONGO_URI" || (echo '请设置 INTEGRATION_MONGO_URI'; exit 1)
	go test -count=1 -race -coverpkg=./internal/... -coverprofile=coverage.out ./tests/unit ./tests/integration
	python3 -m unittest discover -s tests/unit -p '*_test.py'
	node --test tests/frontend/frontend.test.cjs
e2e:
	@test -n "$$INTEGRATION_MONGO_URI" || (echo '请设置 INTEGRATION_MONGO_URI'; exit 1)
	RUN_E2E=1 python3 scripts/with-browser-tools.py go test -count=1 -race -v ./tests/e2e
verify: integration e2e vet build
vet:
	go vet ./...
build:
	go build -o bin/survey-lab ./cmd/server
