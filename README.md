# lookup-go: A Powerful CLI Lookup Tool

`lookup-go` is a command-line utility inspired by Splunk's powerful `lookup` command. It enriches JSON data streams by adding fields based on matching values in an external data source (like a CSV or JSON file). It's designed to be a flexible and high-performance tool for data enrichment pipelines.

The tool reads JSON objects (either as a JSON Array or as JSON Lines) from standard input, performs lookups based on sophisticated rules, and outputs the enriched JSON objects to standard output.

---

## Features

-   **Multiple Data Sources**: Use either **CSV** or **JSON** files as your lookup table.
-   **Advanced Matching Methods**:
    -   `exact`: Case-sensitive or insensitive exact string matching.
    -   `wildcard`: Glob-style wildcard matching (e.g., `bot-*`).
    -   `regex`: Powerful matching using regular expressions.
    -   `cidr`: Match IP addresses against CIDR blocks (e.g., `10.0.0.0/8`).
-   **Flexible Configuration**: A central JSON configuration file separates lookup logic from your data, allowing for complex matching rules.
-   **Built-in DNS Lookup**: Perform forward (`A` record) or reverse (`PTR` record) DNS lookups as a native feature.
    -   Optionally specify a custom DNS server for queries.
-   **Flexible Field Mapping**: Intuitive syntax (`input_field as lookup_field OUTPUT out1 as new1, ...`) to control which fields are matched and how new fields are named.
-   **Handles Multiple Input Formats**: Automatically detects and processes both **JSON Array** and **JSON Lines (JSONL)** from stdin.
-   **Cross-Platform**: Written in Go, it compiles to a single binary with no external dependencies, running on Linux, macOS, and Windows.

---

## Installation

To install `lookup-go`, you need to have Go installed on your system.

1.  Clone the repository or save the source code as `main.go`.
2.  Build the binary:
    ```sh
    go build -o lookup-go main.go
    ```
3.  Place the resulting `lookup-go` binary in a directory in your system's `PATH` (e.g., `/usr/local/bin`).

---

## Usage

The basic command structure is:

```sh
cat input.json | ./lookup-go -c <config.json> -m "<mapping_rule>"
```

### Command-Line Flags

| Flag           | Description                                                                                                                              | Required |
| :------------- | :--------------------------------------------------------------------------------------------------------------------------------------- | :------- |
| `-c <path>`    | Path to the JSON configuration file that defines the data source and matching rules.                                                     | Yes      |
| `-m <string>`  | The mapping rule that specifies how to link input data to the lookup table. (See [Mapping Syntax](#mapping-syntax) below).               | Yes      |
| `--dns`        | Enables DNS lookup mode. When used, the `-c` flag is ignored.                                                                            | No       |
| `--dns-server` | (Optional) Specifies a custom DNS server for DNS lookups (e.g., `8.8.8.8` or `1.1.1.1:53`). If not set, the system's default resolver is used. | No       |

---

## Configuration (`config.json`)

The configuration file is the heart of `lookup-go`, defining where your data is and how to match against it.

### Structure

```json
{
  "data_source": "./path/to/your/data.csv",
  "matchers": [
    {
      "input_field": "field_from_stdin",
      "lookup_field": "column_in_data_source",
      "method": "exact",
      "case_sensitive": false
    },
    {
      "input_field": "another_field_from_stdin",
      "lookup_field": "another_column",
      "method": "regex"
    }
  ]
}
```

-   **`data_source`**: (string) The relative or absolute path to your lookup data file (CSV or JSON).
-   **`matchers`**: (array) A list of objects, where each object defines a specific matching rule.
    -   **`input_field`**: The field name from the incoming JSON stream to use for the lookup.
    -   **`lookup_field`**: The column/key name in your `data_source` file to match against.
    -   **`method`**: The matching algorithm to use. Supported values:
        -   `"exact"` (default)
        -   `"wildcard"`
        -   `"regex"`
        -   `"cidr"`
    -   **`case_sensitive`**: (boolean, optional) If `true`, the match will be case-sensitive. Defaults to `false`. This applies to `exact`, `wildcard`, and `regex` methods.

---

## Mapping Syntax (`-m` flag)

The `-m` flag defines the link between the input stream and the lookup table, and controls the output.

### Format

```
"INPUT_FIELD as LOOKUP_FIELD [OUTPUT original_name1 as new_name1, original_name2 as new_name2]"
```

-   **`INPUT_FIELD as LOOKUP_FIELD`**: (Required)
    -   This part tells `lookup-go` which matcher rule to use from your `config.json`.
    -   `INPUT_FIELD` must match an `input_field` in one of your matchers.
    -   `LOOKUP_FIELD` must match the corresponding `lookup_field` in that same matcher.
-   **`OUTPUT ...`**: (Optional)
    -   This clause controls which fields from the lookup file are added to the output and allows you to rename them.
    -   If the `OUTPUT` clause is **omitted**, all columns from the matched row in the lookup file are added to the JSON object with their original names.

---

## Examples

### Setup

Let's use the following files for our examples.

**`users.csv`** (Data Source)
```csv
username,department,role,building,ip_range
jdoe,Sales,Manager,A,192.168.1.10
asmith,Engineering,Developer,B,192.168.1.25
b-*,Engineering,QA,B,10.0.0.0/8
^scanner-.*$,IT,Service,A,
```

**`lookup_config.json`** (Configuration)
```json
{
  "data_source": "./users.csv",
  "matchers": [
    {
      "input_field": "user",
      "lookup_field": "username",
      "method": "exact",
      "case_sensitive": false
    },
    {
      "input_field": "hostname",
      "lookup_field": "username",
      "method": "wildcard"
    },
    {
      "input_field": "process",
      "lookup_field": "username",
      "method": "regex"
    },
    {
      "input_field": "client_ip",
      "lookup_field": "ip_range",
      "method": "cidr"
    }
  ]
}
```

**`input.jsonl`** (Input Data)
```json
{"timestamp": "2023-10-28T11:00:00Z", "user": "JDOE", "event": "login"}
{"timestamp": "2023-10-28T11:01:00Z", "hostname": "b-jones", "event": "connect"}
{"timestamp": "2023-10-28T11:02:00Z", "process": "scanner-01", "event": "scan"}
{"timestamp": "2023-10-28T11:03:00Z", "client_ip": "10.20.30.40", "event": "access"}
{"timestamp": "2023-10-28T11:04:00Z", "client_ip": "8.8.8.8", "event": "external_access"}
```

### Example 1: Case-Insensitive `exact` Match

Match the `user` field from the input against the `username` column in the CSV, and output the `department` and `role` fields.

```sh
cat input.jsonl | ./lookup-go \
  -c lookup_config.json \
  -m "user as username OUTPUT department as dept, role"
```

**Output (for the first line):**
```json
{"dept":"Sales","event":"login","role":"Manager","timestamp":"2023-10-28T11:00:00Z","user":"JDOE"}
```

### Example 2: `cidr` Match

Match the `client_ip` against the `ip_range` CIDR blocks. We will omit the `OUTPUT` clause to add all fields from the matched CSV row.

```sh
cat input.jsonl | ./lookup-go \
  -c lookup_config.json \
  -m "client_ip as ip_range"
```

**Output (for the fourth line):**
```json
{"building":"B","client_ip":"10.20.30.40","department":"Engineering","event":"access","ip_range":"10.0.0.0/8","role":"QA","timestamp":"2023-10-28T11:03:00Z","username":"b-*"}
```

### Example 3: DNS Lookup

Perform a reverse DNS lookup on the `client_ip` field.

**Command (using system resolver):**
```sh
cat input.jsonl | ./lookup-go \
  --dns \
  -m "client_ip as ignored OUTPUT hostname as resolved_host"
```

**Command (using a custom DNS server):**
```sh
cat input.jsonl | ./lookup-go \
  --dns \
  --dns-server "8.8.8.8" \
  -m "client_ip as ignored OUTPUT hostname as resolved_host"
```

**Output (for the last line, may vary):**
```json
{"client_ip":"8.8.8.8","event":"external_access","resolved_host":"dns.google","timestamp":"2023-10-28T11:04:00Z"}
```
