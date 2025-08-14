# Contributing to Lyzr Crawl

First off, thank you for considering contributing to Lyzr Crawl! It's people like you that make this project such a great tool.

## Code of Conduct

This project and everyone participating in it is governed by our Code of Conduct. By participating, you are expected to uphold this code.

## How Can I Contribute?

### Reporting Bugs

Before creating bug reports, please check existing issues as you might find out that you don't need to create one. When you are creating a bug report, please include as many details as possible:

* Use a clear and descriptive title
* Describe the exact steps which reproduce the problem
* Provide specific examples to demonstrate the steps
* Describe the behavior you observed after following the steps
* Explain which behavior you expected to see instead and why
* Include logs and error messages if applicable

### Suggesting Enhancements

Enhancement suggestions are tracked as GitHub issues. When creating an enhancement suggestion, please include:

* Use a clear and descriptive title
* Provide a step-by-step description of the suggested enhancement
* Provide specific examples to demonstrate the steps
* Describe the current behavior and explain which behavior you expected to see instead
* Explain why this enhancement would be useful

### Pull Requests

* Fill in the required template
* Do not include issue numbers in the PR title
* Follow the Go style guide
* Include thoughtfully-worded, well-structured tests
* Document new code
* End all files with a newline

## Development Setup

1. Fork the repo
2. Clone your fork
3. Create a new branch (`git checkout -b feature/amazing-feature`)
4. Make your changes
5. Run tests (`go test ./...`)
6. Commit your changes (`git commit -m 'Add some amazing feature'`)
7. Push to the branch (`git push origin feature/amazing-feature`)
8. Open a Pull Request

### Local Development

```bash
# Install dependencies
go mod download

# Run tests
go test ./...

# Run with race detector
go test -race ./...

# Run linter (install golangci-lint first)
golangci-lint run

# Generate Swagger docs
swag init -g server.go

# Build
go build -o crawler .
```

## Style Guide

### Go Code Style

* Follow standard Go conventions
* Use `gofmt` to format your code
* Use meaningful variable and function names
* Add comments for exported functions
* Keep functions small and focused
* Handle errors appropriately

### Commit Messages

* Use the present tense ("Add feature" not "Added feature")
* Use the imperative mood ("Move cursor to..." not "Moves cursor to...")
* Limit the first line to 72 characters or less
* Reference issues and pull requests liberally after the first line

Example:
```
Add support for custom user agents

- Allow users to specify custom user agents per crawl
- Add validation for user agent strings
- Update documentation

Fixes #123
```

### Documentation

* Update the README.md with details of changes to the interface
* Update the API documentation if you change endpoints
* Comment your code where necessary
* Write clear commit messages


## Questions?

Feel free to open an issue with your question or contact the maintainers directly.

Thank you for contributing!