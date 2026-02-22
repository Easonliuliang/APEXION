# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Apexion, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, please email the maintainers directly or use [GitHub Security Advisories](https://github.com/apexion-ai/apexion/security/advisories/new) to report privately.

### What to Include

- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

### Response Timeline

- **Acknowledgment**: within 48 hours
- **Initial assessment**: within 1 week
- **Fix and disclosure**: coordinated with reporter

## Supported Versions

| Version | Supported |
|---------|-----------|
| latest  | Yes       |

## Security Considerations

Apexion executes shell commands and interacts with LLM APIs. Users should:

- Never run Apexion with elevated privileges unless necessary
- Review tool calls before approving (especially `bash` and `write_file`)
- Use the permission system (`dangerous` / `safe` levels) appropriately
- Keep API keys in environment variables or config files, never in code
