# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| v0.0.x  | :white_check_mark: |
| < v0.0  | :x:                |

## Reporting a Vulnerability

If you discover a security vulnerability, please **do NOT** create a public GitHub issue.

Instead, please report it by:

1. **Email**: Send details to the maintainers directly (or use GitHub's private vulnerability reporting)
2. **GitHub Security Advisory**: Use [Private Vulnerability Reporting](https://github.com/magicwubiao/go-magic/security/advisories/new)

### What to Include

When reporting, please include:
- Description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

### Response Timeline

- **Initial Response**: Within 48 hours
- **Assessment**: Within 1 week
- **Fix & Release**: As soon as reasonably possible (depending on severity)

### Security Features

go-magic includes several built-in security measures:

- **Command Whitelist**: Only pre-approved commands can be executed
- **Injection Detection**: Prevents malicious input manipulation
- **Dangerous Pattern Blocking**: Blocks known harmful patterns
- **PII Redaction**: Automatically detects and redacts sensitive information

## Security Best Practices

When deploying go-magic:

1. **API Keys**: Never commit API keys to version control
   - Use environment variables or `config.json` with proper file permissions
   - Add `config.json` to `.gitignore`

2. **Command Execution**: Review the command whitelist configuration
   - Only enable commands you actually need
   - Be cautious with shell execution tools

3. **Network**: Run behind appropriate firewall rules
   - The gateway supports TLS configuration
   - Use authentication for exposed endpoints

4. **Updates**: Keep your installation updated
   - Enable Dependabot for automated dependency updates
   - Subscribe to release notifications
