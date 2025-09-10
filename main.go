package main

import (
	"bufio"
	"bytes" // 入力形式の判定のために追加
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// --- データ構造定義 ---

// Config は設定ファイル(config.json)の構造を表します。
type Config struct {
	DataSource string    `json:"data_source"`
	Matchers   []Matcher `json:"matchers"`
}

// Matcher は個々のマッチング規則を定義します。
type Matcher struct {
	InputField     string `json:"input_field"`
	LookupField    string `json:"lookup_field"`
	Method         string `json:"method"` // "exact", "wildcard", "regex", "cidr"
	CaseSensitive  bool   `json:"case_sensitive"`
}

// Mapping はコマンドライン引数 -m のパース結果を保持します。
type Mapping struct {
	InputField  string
	LookupField string
	OutputMap   map[string]string // Key: original output field, Value: new field name
}

// LookupData はCSVやJSONから読み込んだデータの汎用的な表現です。
type LookupData []map[string]string

// --- グローバル変数 ---
var (
	configFilePath = flag.String("c", "", "Path to the lookup configuration JSON file.")
	mappingStr     = flag.String("m", "", "Mapping rule string (e.g., 'field_in as field_lookup OUTPUT out1 as new1')")
	isDnsLookup    = flag.Bool("dns", false, "Enable DNS lookup mode.")
	dnsServerAddr  = flag.String("dns-server", "", "Custom DNS server address (e.g., '8.8.8.8:53'). Uses system default if not set.")
	showVersion    = flag.Bool("version", false, "Print version and exit")
)

// version はビルド時にldflagsで注入されます。
var version = "dev"

// --- main関数 ---
func main() {
	// サブコマンドのチェック
	if len(os.Args) > 1 && os.Args[1] == "generate-config" {
		handleGenerateConfig()
		return // generate-configが実行されたらここで終了
	}

	// カスタムのヘルプメッセージを設定
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, `lookup-go: Enrich JSON/JSONL data by looking up values from external data sources.

Usage:
  lookup-go -c <config.json> -m "<mapping_rule>" < input.jsonl
  lookup-go --dns -m "<mapping_rule>" < input.jsonl
  lookup-go generate-config -file <data_source.csv/json> > config.json
  lookup-go --version

Description:
  This tool reads JSON or JSONL data from stdin, looks up values based on a specified field,
  and appends information from an external data source (CSV or JSON) or DNS to the output.

Subcommands:
  generate-config
    Generates a template for the configuration file from a data source file.
    Options:
      -file string
            Path to the data source file (CSV or JSON). (Required)

Options:
`)
		flag.PrintDefaults()
		fmt.Fprintf(os.Stderr, `
Mapping Rule (-m):
  The mapping rule defines which fields to use for the lookup and how to map the output fields.
  Format: "<input_field> as <lookup_field> OUTPUT <source_field1> as <target_field1>, <source_field2> as <target_field2>, ..."

  - <input_field>:  Field name in the stdin JSON to use for the lookup.
  - <lookup_field>: Field name in the data source to match against.
  - OUTPUT:         Keyword to start defining output field mappings.
  - <source_field>: Field name from the data source to append to the output.
  - <target_field>: New field name for the appended data. If "as <target_field>" is omitted,
                    the source_field name is used.

Examples:
  # 1. Basic Lookup
  #    Lookup 'user_id' from stdin in 'users.csv' and append 'user_name' and 'email' as new fields.
  $ cat input.jsonl | lookup-go -c lookup_config.json -m "user_id as id OUTPUT user_name as name, email"

  # 2. Generate Config
  #    Generate a config template from 'users.csv'.
  $ lookup-go generate-config -file users.csv > lookup_config.json

  # 3. DNS Lookup
  #    Perform a DNS lookup for the IP address in the 'client_ip' field.
  $ echo '{"client_ip":"8.8.8.8"}' | lookup-go --dns -m "client_ip as ip OUTPUT hostname"

`)
	}

	flag.Parse()
	log.SetOutput(os.Stderr)

	if *showVersion {
		fmt.Printf("lookup-go version %s\n", version)
		os.Exit(0)
	}

	if *mappingStr == "" {
		log.Fatal("Error: -m (mapping) flag is required.")
	}
	if !*isDnsLookup && *configFilePath == "" {
		log.Fatal("Error: -c (config) flag is required unless --dns is specified.")
	}
	if *isDnsLookup && *configFilePath != "" {
		log.Println("Warning: -c flag is ignored when --dns is specified.")
	}

	mapping, err := parseMapping(*mappingStr)
	if err != nil {
		log.Fatalf("Error parsing mapping rule: %v", err)
	}

	var lookupData LookupData
	var matcher *Matcher

	if !*isDnsLookup {
		config, err := loadConfig(*configFilePath)
		if err != nil {
			log.Fatalf("Error loading config file: %v", err)
		}

		for i := range config.Matchers {
			m := &config.Matchers[i]
			if m.InputField == mapping.InputField && m.LookupField == mapping.LookupField {
				matcher = m
				break
			}
		}
		if matcher == nil {
			log.Fatalf("Error: No matcher found in config for input_field='%s' and lookup_field='%s'", mapping.InputField, mapping.LookupField)
		}

		dataSourcePath := resolveDataSourcePath(*configFilePath, config.DataSource)
		ext := filepath.Ext(dataSourcePath)
		switch strings.ToLower(ext) {
		case ".csv":
			lookupData, err = loadLookupDataFromCSV(dataSourcePath)
		case ".json", ".jsonl":
			lookupData, err = loadLookupDataFromJSON(dataSourcePath)
		default:
			err = fmt.Errorf("unsupported data_source format '%s'", ext)
		}
		if err != nil {
			log.Fatalf("Error loading data source: %v", err)
		}
	}

	processInput(mapping, lookupData, matcher)
}

// processInput は標準入力の形式を自動検出し、処理を振り分けます。
func processInput(mapping *Mapping, lookupData LookupData, matcher *Matcher) {
	inputBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatalf("Error reading from stdin: %v", err)
	}

	trimmedInput := bytes.TrimSpace(inputBytes)
	if len(trimmedInput) == 0 {
		return
	}

	// JSON配列形式の場合
	if trimmedInput[0] == '[' {
		var dataArray []map[string]interface{}
		if err := json.Unmarshal(trimmedInput, &dataArray); err != nil {
			log.Fatalf("Error parsing JSON array: %v", err)
		}

		var resultsArray []map[string]interface{}
		for _, data := range dataArray {
			processedData := processObject(data, mapping, lookupData, matcher)
			resultsArray = append(resultsArray, processedData)
		}

		// 結果を整形してJSON配列として出力
		output, err := json.MarshalIndent(resultsArray, "", "  ")
		if err != nil {
			log.Fatalf("Error marshalling result array to JSON: %v", err)
		}
		fmt.Println(string(output))

	// JSONL (または単一のJSON) 形式の場合
	} else {
		scanner := bufio.NewScanner(bytes.NewReader(inputBytes))
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}

			var data map[string]interface{}
			if err := json.Unmarshal(line, &data); err != nil {
				log.Printf("Warning: Could not parse line as JSON, skipping: %s", string(line))
				continue
			}

			processedData := processObject(data, mapping, lookupData, matcher)
			printJSON(processedData)
		}
		if err := scanner.Err(); err != nil {
			log.Fatalf("Error scanning input: %v", err)
		}
	}
}

// processObject は単一のJSONオブジェクトに対してルックアップ処理を行います。
func processObject(data map[string]interface{}, mapping *Mapping, lookupData LookupData, matcher *Matcher) map[string]interface{} {
	inputValue, ok := data[mapping.InputField]
	if !ok {
		return data
	}
	inputValueStr, ok := inputValue.(string)
	if !ok {
		return data
	}

	var lookupResult map[string]string
	if *isDnsLookup {
		dnsRes := performDnsLookup(inputValueStr, *dnsServerAddr)
		if dnsRes != nil {
			lookupResult = make(map[string]string)
			for k, v := range dnsRes {
				lookupResult[k] = fmt.Sprintf("%v", v)
			}
		}
	} else {
		lookupResult = findMatch(inputValueStr, lookupData, matcher)
	}

	if lookupResult != nil {
		for originalKey, value := range lookupResult {
			newKey, exists := mapping.OutputMap[originalKey]
			if !exists && len(mapping.OutputMap) == 0 {
				newKey = originalKey
				exists = true
			}
			if exists {
				data[newKey] = value
			}
		}
	}
	return data
}

// findMatch は設定に基づき、データソース内で一致するエントリを探します。
func findMatch(value string, data LookupData, matcher *Matcher) map[string]string {
	for _, row := range data {
		lookupValue, ok := row[matcher.LookupField]
		if !ok {
			continue
		}

		compareValue := value
		compareLookupValue := lookupValue

		if !matcher.CaseSensitive {
			compareValue = strings.ToLower(compareValue)
			compareLookupValue = strings.ToLower(compareLookupValue)
		}

		var matched bool
		var err error

		switch matcher.Method {
		case "exact":
			matched = (compareValue == compareLookupValue)
		case "wildcard":
			matched, err = filepath.Match(compareLookupValue, compareValue)
		case "regex":
			matched, err = regexp.MatchString(compareLookupValue, compareValue)
		case "cidr":
			ip := net.ParseIP(compareValue)
			if ip != nil {
				_, cidrNet, parseErr := net.ParseCIDR(compareLookupValue)
				if parseErr == nil && cidrNet.Contains(ip) {
					matched = true
				}
			}
		default:
			log.Printf("Warning: Unknown match method '%s'", matcher.Method)
			return nil
		}

		if err != nil {
			log.Printf("Warning: Error during match (method: %s, pattern: %s): %v", matcher.Method, lookupValue, err)
			continue
		}

		if matched {
			return row
		}
	}
	return nil
}

// performDnsLookup はDNSの正引き・逆引きを行います。
func performDnsLookup(value string, serverAddr string) map[string]interface{} {
	result := make(map[string]interface{})

	if serverAddr != "" {
		resolver := &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{}
				addr := serverAddr
				if !strings.Contains(addr, ":") {
					addr = addr + ":53"
				}
				return d.DialContext(ctx, "udp", addr)
			},
		}
		ctx := context.Background()

		if ip := net.ParseIP(value); ip != nil {
			names, err := resolver.LookupAddr(ctx, value)
			if err == nil && len(names) > 0 {
				result["hostname"] = strings.TrimSuffix(names[0], ".")
				return result
			}
		} else {
			addrs, err := resolver.LookupHost(ctx, value)
			if err == nil && len(addrs) > 0 {
				result["ip"] = addrs[0]
				return result
			}
		}
		return nil
	}

	if ip := net.ParseIP(value); ip != nil {
		names, err := net.LookupAddr(value)
		if err == nil && len(names) > 0 {
			result["hostname"] = strings.TrimSuffix(names[0], ".")
			return result
		}
	} else {
		addrs, err := net.LookupHost(value)
		if err == nil && len(addrs) > 0 {
			result["ip"] = addrs[0]
			return result
		}
	}
	return nil
}

// --- ヘルパー関数 ---

func loadConfig(path string) (*Config, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read config file: %w", err)
	}
	var config Config
	if err := json.Unmarshal(file, &config); err != nil {
		return nil, fmt.Errorf("could not parse config JSON: %w", err)
	}
	for i := range config.Matchers {
		if config.Matchers[i].Method == "" {
			config.Matchers[i].Method = "exact"
		}
	}
	return &config, nil
}

func resolveDataSourcePath(configPath, dataSource string) string {
	if strings.HasPrefix(dataSource, "~/") {
		homeDir, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(homeDir, dataSource[2:])
		}
	}
	if filepath.IsAbs(dataSource) {
		return dataSource
	}
	return filepath.Join(filepath.Dir(configPath), dataSource)
}

func parseMapping(m string) (*Mapping, error) {
	re := regexp.MustCompile(`^(\S+)\s+as\s+(\S+)(\s+OUTPUT\s+(.*))?$`)
	matches := re.FindStringSubmatch(m)
	if len(matches) < 3 {
		return nil, fmt.Errorf("invalid mapping format: %s", m)
	}
	mapping := &Mapping{
		InputField:  matches[1],
		LookupField: matches[2],
		OutputMap:   make(map[string]string),
	}
	if len(matches) > 4 && matches[4] != "" {
		outputPairs := strings.Split(matches[4], ",")
		for _, pair := range outputPairs {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			parts := regexp.MustCompile(`\s+as\s+`).Split(pair, 2)
			if len(parts) == 2 {
				mapping.OutputMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			} else {
				mapping.OutputMap[pair] = pair
			}
		}
	}
	return mapping, nil
}

func loadLookupDataFromCSV(path string) (LookupData, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not open file: %w", err)
	}
	defer file.Close()
	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("could not read CSV header: %w", err)
	}
	var data LookupData
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("error reading CSV record: %w", err)
		}
		row := make(map[string]string)
		for i, value := range record {
			if i < len(header) {
				row[header[i]] = value
			}
		}
		data = append(data, row)
	}
	return data, nil
}

func loadLookupDataFromJSON(path string) (LookupData, error) {
	file, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read file: %w", err)
	}
	var rawData []map[string]interface{}
	if err := json.Unmarshal(file, &rawData); err != nil {
		return nil, fmt.Errorf("could not parse JSON: %w", err)
	}
	var data LookupData
	for _, rawRow := range rawData {
		row := make(map[string]string)
		for key, val := range rawRow {
			row[key] = fmt.Sprintf("%v", val)
		}
		data = append(data, row)
	}
	return data, nil
}

func printJSON(data map[string]interface{}) {
	output, err := json.Marshal(data)
	if err != nil {
		log.Printf("Warning: Could not marshal result to JSON, skipping: %v", err)
		return
	}
	fmt.Println(string(output))
}

// --- 雛形生成機能 ---

// handleGenerateConfig は generate-config サブコマンドの引数を処理し、実行します。
func handleGenerateConfig() {
	genCmd := flag.NewFlagSet("generate-config", flag.ExitOnError)
	filePath := genCmd.String("file", "", "Path to the data source file (CSV or JSON).")
	genCmd.Parse(os.Args[2:])

	if *filePath == "" {
		log.Fatal("Error: -file flag is required for generate-config command.")
	}

	var headers []string
	var err error

	ext := filepath.Ext(*filePath)
	switch strings.ToLower(ext) {
	case ".csv":
		headers, err = extractHeadersFromCSV(*filePath)
	case ".json", ".jsonl":
		headers, err = extractKeysFromJSON(*filePath)
	default:
		log.Fatalf("Error: Unsupported file type '%s'. Only .csv, .json, and .jsonl are supported.", ext)
	}

	if err != nil {
		log.Fatalf("Error processing file %s: %v", *filePath, err)
	}

	config := Config{
		DataSource: *filePath,
		Matchers:   make([]Matcher, 0, len(headers)),
	}

	for _, header := range headers {
		config.Matchers = append(config.Matchers, Matcher{
			InputField:    header,
			LookupField:   header,
			Method:        "exact",
			CaseSensitive: false,
		})
	}

	output, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		log.Fatalf("Error generating json output: %v", err)
	}

	fmt.Println(string(output))
}

// extractHeadersFromCSV はCSVファイルのヘッダーを抽出します。
func extractHeadersFromCSV(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not open file: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("could not read CSV header: %w", err)
	}
	return header, nil
}

// extractKeysFromJSON はJSONファイル内のすべてのキーをスキャンして抽出します。
// JSON配列とJSONLの両方に対応します。
func extractKeysFromJSON(path string) ([]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not open file: %w", err)
	}
	defer file.Close()

	// ファイルの先頭を少し読んで、JSON配列かJSONLかを判断する
	reader := bufio.NewReader(file)
	firstBytes, err := reader.Peek(512) // 先頭512バイトを覗き見
	if err != nil && err != io.EOF {
		// ファイルが512バイトより小さい場合、EOFは期待されるがエラーではない
	}

	// ファイルの読み取り位置を先頭に戻す
	_, err = file.Seek(0, 0)
	if err != nil {
		return nil, fmt.Errorf("could not seek back to the beginning of the file: %w", err)
	}

	allKeys := make(map[string]struct{})

	trimmedBytes := bytes.TrimSpace(firstBytes)
	// JSON配列かどうかをチェック
	if len(trimmedBytes) > 0 && trimmedBytes[0] == '[' {
		var arr []map[string]interface{}
		decoder := json.NewDecoder(file)
		if err := decoder.Decode(&arr); err != nil {
			return nil, fmt.Errorf("could not parse JSON array: %w", err)
		}
		for _, obj := range arr {
			for k := range obj {
				allKeys[k] = struct{}{}
			}
		}
	} else { // JSONLと仮定
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			line := scanner.Bytes()
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var data map[string]interface{}
			if err := json.Unmarshal(line, &data); err != nil {
				// 有効なJSONオブジェクトではない行は無視する
				continue
			}
			for k := range data {
				allKeys[k] = struct{}{}
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, fmt.Errorf("error scanning JSONL file: %w", err)
		}
	}

	if len(allKeys) == 0 {
		return nil, fmt.Errorf("no keys found in JSON file")
	}

	keys := make([]string, 0, len(allKeys))
	for k := range allKeys {
		keys = append(keys, k)
	}
	return keys, nil
}