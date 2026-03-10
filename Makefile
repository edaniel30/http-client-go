.PHONY: test test-unit test-integration test-coverage test-coverage-html test-race setup clean

COVERAGE_THRESHOLD=80
COVERAGE_FILE=coverage.out

test:
	@echo "Running all tests..."
	@go test -v ./...

test-unit:
	@echo "Running unit tests only (fast, no Docker)..."
	@go test -short -v ./...

test-integration:
	@echo "Running integration tests (requires Docker)..."
	@go test -v -run Integration ./...

test-coverage:
	@echo "🧪 Running tests with coverage (this may take 1-2 minutes)..."
	@echo "⏳ Starting Docker containers and running tests..."
	@EXCLUDE_PATTERN=$$(cat .coverignore | grep -v '^#' | grep -v '^$$' | sed 's/^/-e /' | tr '\n' ' '); \
	go test -v -coverprofile=$(COVERAGE_FILE) $$(go list ./... | grep -v $$EXCLUDE_PATTERN) | grep -E "(PASS|FAIL|RUN|---|===|coverage:)"
	@echo ""
	@echo "📊 Generating coverage report..."
	@echo ""
	@echo "=== Coverage by function ==="
	@go tool cover -func=$(COVERAGE_FILE)
	@echo ""
	@echo "=== Coverage Summary ==="
	@COVERAGE=$$(go tool cover -func=$(COVERAGE_FILE) | grep total | awk '{print $$3}' | sed 's/%//'); \
	echo "Total coverage: $$COVERAGE%"; \
	echo "Threshold: $(COVERAGE_THRESHOLD)%"; \
	if [ $$(echo "$$COVERAGE < $(COVERAGE_THRESHOLD)" | bc -l) -eq 1 ]; then \
		echo ""; \
		echo "❌ FAIL: Coverage $$COVERAGE% is below $(COVERAGE_THRESHOLD)%"; \
		exit 1; \
	else \
		echo ""; \
		echo "✅ PASS: Coverage meets threshold"; \
	fi

test-coverage-html:
	@echo "🧪 Running tests with coverage (this may take 1-2 minutes)..."
	@echo "⏳ Starting Docker containers and running tests..."
	@EXCLUDE_PATTERN=$$(cat .coverignore | grep -v '^#' | grep -v '^$$' | sed 's/^/-e /' | tr '\n' ' '); \
	go test -v -coverprofile=$(COVERAGE_FILE) $$(go list ./... | grep -v $$EXCLUDE_PATTERN) | grep -E "(PASS|FAIL|RUN|---|===|coverage:)"
	@echo ""
	@echo "📊 Generating coverage report..."
	@echo ""
	@echo "=== Coverage by function ==="
	@go tool cover -func=$(COVERAGE_FILE)
	@echo ""
	@echo "=== Coverage Summary ==="
	@COVERAGE=$$(go tool cover -func=$(COVERAGE_FILE) | grep total | awk '{print $$3}' | sed 's/%//'); \
	echo "Total coverage: $$COVERAGE%"; \
	echo "Threshold: $(COVERAGE_THRESHOLD)%"; \
	if [ $$(echo "$$COVERAGE < $(COVERAGE_THRESHOLD)" | bc -l) -eq 1 ]; then \
		echo ""; \
		echo "❌ FAIL: Coverage $$COVERAGE% is below $(COVERAGE_THRESHOLD)%"; \
	else \
		echo ""; \
		echo "✅ PASS: Coverage meets threshold"; \
	fi
	@echo ""
	@echo "🌐 Generating HTML coverage report..."
	@go tool cover -html=$(COVERAGE_FILE) -o coverage.html
	@echo "📂 Opening coverage report..."
	@open coverage.html

test-race:
	@echo "Running tests with race detector..."
	@go test -race -v ./...

setup:
	@echo "Installing pre-commit hooks..."
	@pre-commit install
	@echo "Setup complete!"

pre-commit:
	@echo "Running pre-commit checks on all files..."
	@pre-commit run --all-files

clean:
	@rm -f $(COVERAGE_FILE) coverage.html
