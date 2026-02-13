.PHONY: all build test clean install uninstall run dev help install-systemd uninstall-systemd install-daemon uninstall-daemon installer

BINARY_NAME=moonbit
INSTALLER_NAME=moonbit-installer
INSTALL_PATH=/usr/local/bin
SYSTEMD_PATH=/etc/systemd/system
VERSION=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u '+%Y-%m-%d_%H:%M:%S')
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.BuildTime=$(BUILD_TIME)"

# Default target
all: build

# Build the binary
build:
	@echo "Building $(BINARY_NAME)..."
	go build $(LDFLAGS) -o $(BINARY_NAME) cmd/main.go
	@echo "Build complete: ./$(BINARY_NAME)"

# Run tests
test:
	@echo "Running tests..."
	go test ./... -v

# Run tests with coverage
test-coverage:
	@echo "Running tests with coverage..."
	go test -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

# Run tests with race detector
test-race:
	@echo "Running tests with race detector..."
	go test ./... -race -v

# Run all tests (unit + race + coverage)
test-all: test test-race test-coverage

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f $(BINARY_NAME)
	rm -f $(INSTALLER_NAME)
	rm -f coverage.out coverage.html
	@echo "Clean complete"

# Install binary to system
install: build
	@echo "Installing $(BINARY_NAME) to $(INSTALL_PATH)..."
	sudo cp $(BINARY_NAME) $(INSTALL_PATH)/
	sudo chmod +x $(INSTALL_PATH)/$(BINARY_NAME)
	@echo "Installed: $(INSTALL_PATH)/$(BINARY_NAME)"

# Uninstall binary from system
uninstall:
	@echo "Uninstalling $(BINARY_NAME)..."
	sudo rm -f $(INSTALL_PATH)/$(BINARY_NAME)
	@echo "Uninstalled"

# Install systemd service and timer files
install-systemd:
	@echo "Installing systemd service files..."
	sudo cp systemd/moonbit-scan.service $(SYSTEMD_PATH)/
	sudo cp systemd/moonbit-scan.timer $(SYSTEMD_PATH)/
	sudo cp systemd/moonbit-clean.service $(SYSTEMD_PATH)/
	sudo cp systemd/moonbit-clean.timer $(SYSTEMD_PATH)/
	sudo systemctl daemon-reload
	@echo "Systemd files installed. Enable with:"
	@echo "  sudo systemctl enable --now moonbit-scan.timer"
	@echo "  sudo systemctl enable --now moonbit-clean.timer"

# Uninstall systemd service and timer files
uninstall-systemd:
	@echo "Uninstalling systemd service files..."
	sudo systemctl disable --now moonbit-scan.timer 2>/dev/null || true
	sudo systemctl disable --now moonbit-clean.timer 2>/dev/null || true
	sudo rm -f $(SYSTEMD_PATH)/moonbit-scan.service
	sudo rm -f $(SYSTEMD_PATH)/moonbit-scan.timer
	sudo rm -f $(SYSTEMD_PATH)/moonbit-clean.service
	sudo rm -f $(SYSTEMD_PATH)/moonbit-clean.timer
	sudo systemctl daemon-reload
	@echo "Systemd files uninstalled"

# Install daemon service (long-running mode)
install-daemon: build
	@echo "Installing moonbit daemon service..."
	sudo cp $(BINARY_NAME) $(INSTALL_PATH)/
	sudo chmod +x $(INSTALL_PATH)/$(BINARY_NAME)
	sudo mkdir -p /var/log/moonbit
	sudo cp systemd/moonbit-daemon.service $(SYSTEMD_PATH)/
	sudo systemctl daemon-reload
	sudo systemctl enable --now moonbit-daemon.service
	@echo "moonbit daemon installed and started"

# Uninstall daemon service
uninstall-daemon:
	@echo "Uninstalling moonbit daemon..."
	-sudo systemctl disable --now moonbit-daemon.service
	-sudo rm -f $(SYSTEMD_PATH)/moonbit-daemon.service
	sudo systemctl daemon-reload
	@echo "moonbit daemon removed"

# Run the application (interactive TUI mode)
run: build
	./$(BINARY_NAME)

# Development mode - build and run with sudo
dev: build
	sudo ./$(BINARY_NAME)

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Run linter
lint:
	@echo "Running linter..."
	@which golangci-lint > /dev/null || (echo "golangci-lint not installed. Install: https://golangci-lint.run/usage/install/" && exit 1)
	golangci-lint run

# Tidy dependencies
tidy:
	@echo "Tidying dependencies..."
	go mod tidy

# Vendor dependencies
vendor:
	@echo "Vendoring dependencies..."
	go mod vendor

# Build for multiple platforms
build-all:
	@echo "Building for multiple platforms..."
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-linux-amd64 cmd/main.go
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_NAME)-linux-arm64 cmd/main.go
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BINARY_NAME)-darwin-amd64 cmd/main.go
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BINARY_NAME)-darwin-arm64 cmd/main.go
	@echo "Multi-platform build complete"

# Build the installer
installer:
	@echo "Building $(INSTALLER_NAME)..."
	go build $(LDFLAGS) -o $(INSTALLER_NAME) cmd/installer/main.go
	@echo "Build complete: ./$(INSTALLER_NAME)"

# Show help
help:
	@echo "MoonBit - System Cleaner Makefile"
	@echo ""
	@echo "Usage:"
	@echo "  make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  build          Build the binary (default)"
	@echo "  test           Run unit tests"
	@echo "  test-coverage  Run tests with coverage report"
	@echo "  test-race      Run tests with race detector"
	@echo "  test-all       Run all test suites"
	@echo "  clean          Remove build artifacts"
	@echo "  install        Install binary to $(INSTALL_PATH)"
	@echo "  uninstall      Remove binary from $(INSTALL_PATH)"
	@echo "  install-systemd   Install systemd service/timer files"
	@echo "  uninstall-systemd Remove systemd service/timer files"
	@echo "  install-daemon Install daemon service (long-running mode)"
	@echo "  uninstall-daemon  Remove daemon service"
	@echo "  installer      Build the installer TUI"
	@echo "  run            Build and run in TUI mode"
	@echo "  dev            Build and run with sudo (for system paths)"
	@echo "  fmt            Format code with go fmt"
	@echo "  lint           Run golangci-lint"
	@echo "  tidy           Tidy go.mod dependencies"
	@echo "  vendor         Vendor dependencies"
	@echo "  build-all      Build for multiple platforms"
	@echo "  help           Show this help message"
