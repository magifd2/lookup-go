# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.3.0] - 2025-09-10

### Added

-   `~` (tilde) in the `data_source` path of the configuration file is now expanded to the user's home directory.

## [1.2.0] - 2025-09-10

### Changed

-   **Improved Help Message**: The command-line help (`--help`) has been significantly enhanced to be more user-friendly. It now includes a detailed description, usage patterns, subcommand explanations, and practical examples to make the tool easier to understand and use.

## [1.1.0] - 2025-09-10

### Added

-   **`generate-config` Subcommand**: A new helper command to automatically generate a configuration file template from a given data source (`.csv`, `.json`, or `.jsonl`). This simplifies the initial setup process.
    -   It intelligently scans the entire data file to find all possible lookup keys.
    -   It automatically populates the `input_field` and `lookup_field` in the generated template.

## [1.0.0] - 2025-08-26

This is the initial public release of `lookup-go`.

### Added

-   **Core Lookup Functionality**: Enrich JSON/JSONL data from stdin by looking up values in external CSV or JSON data sources.
-   **Advanced Matching Methods**: 
    -   `exact`: Case-sensitive or insensitive exact string matching.
    -   `wildcard`: Glob-style wildcard matching.
    -   `regex`: Regular expression matching.
    -   `cidr`: IP address against CIDR block matching.
-   **DNS Lookup Mode**: Perform forward (A) or reverse (PTR) DNS lookups as a native feature, with support for custom DNS servers.
-   **Flexible I/O**: Automatically handles both JSON Array and JSON Lines (JSONL) input formats.
-   **Robust Build System**: A comprehensive `Makefile` for testing, building, cross-compiling, and packaging releases.
-   **Automated Testing**: A full black-box test suite (`make test`) to ensure reliability.
-   **MIT License**: The project is licensed under the MIT License.
-   **Initial Documentation**: A `README.md` file with detailed usage instructions and examples.