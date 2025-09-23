# Contributing Guidelines

## Commit Messages

This project follows the [Conventional Commits](https://www.conventionalcommits.org/) specification.

Use the format:

```
<type>(<optional scope>): <short description>
```

Examples:

- `feat(registry): add prom metrics for pushes`
- `fix: handle missing namespace`

Commonly used types include `feat`, `fix`, `docs`, `chore`, `test`, `refactor`, `ci`, and `build`.

## Optional Git Hook

To help you follow the convention automatically, install the provided git hook:

```bash
ln -s ../../hack/commit-msg .git/hooks/commit-msg
```

The hook rejects commits whose first line does not match the required pattern. Remove the symlink to disable it.

## Code Quality

Run the linters and tests before submitting changes:

```bash
make lint
make test
```
