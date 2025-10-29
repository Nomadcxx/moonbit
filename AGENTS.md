# Agent Guidelines for Moonbit

## Build/Lint/Test Commands

### Build
```bash
go build ./...                    # Build all packages
go build -o moonbit ./cmd/main.go # Build main binary
go mod tidy                       # Clean dependencies
```

### Lint/Format
```bash
golangci-lint run               # Run golangci-lint (CI standard)
go vet ./...                    # Basic Go vet
go fmt ./...                    # Format code
gofmt -d .                      # Show formatting diffs
```

### Test (if tests exist)
```bash
go test ./...                   # Run all tests
go test -v ./internal/scanner   # Run tests for specific package
go test -run TestScanner        # Run single test
```

## Code Style Guidelines

### Imports
- Use grouped imports (stdlib, external, internal)
- External imports: github.com/BurntSushi/toml, github.com/charmbracelet/bubbletea, github.com/spf13/cobra
- Internal imports: github.com/Nomadcxx/moonbit/internal/...

### Naming Conventions
- PascalCase for types, interfaces, exported functions
- camelCase for variables, functions, fields
- Snake case for TOML config fields

### Error Handling
- Use fmt.Errorf with %w for wrapping errors
- Return errors early, handle them at appropriate levels
- Use context.Context for cancellation

### Types & Structs
- Use struct tags for TOML: `toml:"field_name"`
- Prefer specific types over interface{} where possible
- Use time.Duration for timing, uint64 for sizes

### Architecture
- Clear separation: cmd/, internal/, config/, scanner/, ui/, themes/
- Dependency injection via constructors
- Channel-based progress reporting