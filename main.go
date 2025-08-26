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
		case ".json":
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