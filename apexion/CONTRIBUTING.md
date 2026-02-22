# Contributing to Apexion

Thank you for your interest in contributing to Apexion!

## Getting Started

1. Fork and clone the repository
2. Install Go 1.24+
3. Build the project:

```bash
make build
```

4. Run tests:

```bash
make test
```

## Development Workflow

1. Create a feature branch from `main`
2. Make your changes
3. Add tests for new functionality
4. Ensure all tests pass: `make test`
5. Run linter: `make lint`
6. Commit with a clear message (see below)
7. Open a Pull Request

## Commit Messages

We follow [Conventional Commits](https://www.conventionalcommits.org/):

- `feat:` new feature
- `fix:` bug fix
- `docs:` documentation only
- `refactor:` code change that neither fixes a bug nor adds a feature
- `test:` adding or updating tests
- `chore:` maintenance tasks

Examples:

```
feat: add OpenRouter provider support
fix: handle empty API response gracefully
docs: update configuration examples
```

## Code Style

- Follow standard Go conventions (`gofmt`, `go vet`)
- Exported functions must have godoc comments
- Keep functions focused and small
- Error messages should be lowercase without trailing punctuation

## Pull Request Guidelines

- Keep PRs focused on a single change
- Include tests for new functionality
- Update documentation if behavior changes
- Fill out the PR template

## Reporting Issues

- Use the GitHub issue templates
- Include steps to reproduce for bugs
- Include your Go version and OS

## License

By contributing, you agree that your contributions will be licensed under the Apache License 2.0.
