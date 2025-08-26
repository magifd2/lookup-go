# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.0.0] - 2025-08-26

This is the initial public release of `lookup-go`.

### Added

- **Core Lookup Functionality**: Enrich JSON/JSONL data from stdin by looking up values in external CSV or JSON data sources.
- **Advanced Matching Methods**: 
  - `exact`: Case-sensitive or insensitive exact string matching.
  - `wildcard`: Glob-style wildcard matching.
  - `regex`: Regular expression matching.
  - `cidr`: IP address against CIDR block matching.
- **DNS Lookup Mode**: Perform forward (A) or reverse (PTR) DNS lookups as a native feature, with support for custom DNS servers.
- **Flexible I/O**: Automatically handles both JSON Array and JSON Lines (JSONL) input formats.
- **Robust Build System**: A comprehensive `Makefile` for testing, building, cross-compiling, and packaging releases.
- **Automated Testing**: A full black-box test suite (`make test`) to ensure reliability.
- **MIT License**: The project is licensed under the MIT License.
- **Initial Documentation**: A `README.md` file with detailed usage instructions and examples.
