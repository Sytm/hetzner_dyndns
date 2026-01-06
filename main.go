package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"slices"
)

type DynDnsConfig struct {
	HetznerApiKey string
	ZoneName      string
	RecordName    string
	RecordTTL     int
	A             RecordConfig
	AAAA          RecordConfig
}

type RecordConfig struct {
	Enabled bool
	Source  string
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("config file not provided")
		return
	}
	log.Println("Reading config at", os.Args[1])
	config := readConfig(os.Args[1])

	processRecord(config, "A", &config.A)
	processRecord(config, "AAAA", &config.AAAA)
}

func readConfig(configPath string) *DynDnsConfig {
	configFile, err := os.OpenFile(configPath, os.O_RDONLY, 0600)

	defer func(configFile *os.File) {
		_ = configFile.Close()
	}(configFile)

	if err != nil {
		log.Fatalln("could not open config file", err)
	}

	decoder := json.NewDecoder(configFile)
	config := &DynDnsConfig{
		RecordTTL: 600,
		A: RecordConfig{
			Source: "https://ipv4.seeip.org",
		},
		AAAA: RecordConfig{
			Source: "https://ipv6.seeip.org",
		},
	}

	err = decoder.Decode(config)
	if err != nil {
		log.Fatalln("could not parse config file", err)
	}

	return config
}

func processRecord(config *DynDnsConfig, recordType string, recordConfig *RecordConfig) {
	if !recordConfig.Enabled {
		return
	}

	ipString := getPublicIP(recordConfig)
	if parsedIp := net.ParseIP(ipString); parsedIp == nil || ((recordType == "A") == (parsedIp.To4() == nil)) {
		log.Fatalf("service returned invalid ip address %s", ipString)
	}

	if checkRecordExists(config, recordType) {
		updateRecord(config, recordType, ipString)
	} else {
		createRecord(config, recordType, ipString)
	}
}

func getPublicIP(recordConfig *RecordConfig) string {
	res, err := http.Get(recordConfig.Source)
	if err != nil {
		log.Fatalf("could not fetch ip from %s %v\n", recordConfig.Source, err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(res.Body)

	ip, err := io.ReadAll(res.Body)
	if err != nil {
		log.Fatalln("could not read response", err)
	}

	return string(ip)
}

type rrSetPayload struct {
	Name    string        `json:"name,omitempty"`
	Type    string        `json:"type,omitempty"`
	TTL     int           `json:"ttl,omitempty"`
	Records []rrSetRecord `json:"records"`
}
type rrSetRecord struct {
	Value string `json:"value"`
}

func checkRecordExists(config *DynDnsConfig, recordType string) bool {
	endpoint := fmt.Sprintf("https://api.hetzner.cloud/v1/zones/%s/rrsets/%s/%s", config.ZoneName, config.RecordName, recordType)

	statusCode, err := doAuthenticated("GET", config.HetznerApiKey, endpoint, nil, []int{200, 404})

	if err != nil {
		log.Fatalln("could not check record existence", err)
	}

	return statusCode == 200
}

func createRecord(config *DynDnsConfig, recordType string, publicIp string) {
	log.Printf("creating record of type %s with %s\n", recordType, publicIp)
	endpoint := fmt.Sprintf("https://api.hetzner.cloud/v1/zones/%s/rrsets", config.ZoneName)

	payload := &rrSetPayload{
		Name: config.RecordName,
		Type: recordType,
		TTL:  config.RecordTTL,
		Records: []rrSetRecord{
			{
				Value: publicIp,
			},
		},
	}

	_, err := doAuthenticated("POST", config.HetznerApiKey, endpoint, payload, []int{201})

	if err != nil {
		log.Fatalf("could not create record of type %s with %s %v\n", recordType, publicIp, err)
	}
}

func updateRecord(config *DynDnsConfig, recordType string, publicIp string) {
	log.Printf("updating record of type %s to %s\n", recordType, publicIp)
	endpoint := fmt.Sprintf("https://api.hetzner.cloud/v1/zones/%s/rrsets/%s/%s/actions/set_records", config.ZoneName, config.RecordName, recordType)

	payload := &rrSetPayload{
		Records: []rrSetRecord{
			{
				Value: publicIp,
			},
		},
	}

	_, err := doAuthenticated("POST", config.HetznerApiKey, endpoint, payload, []int{201})

	if err != nil {
		log.Fatalf("could not update record of type %s to %s %v", recordType, publicIp, err)
	}
}

func doAuthenticated(method string, apiKey string, url string, payload *rrSetPayload, expectedStatusCodes []int) (int, error) {
	var body io.Reader = http.NoBody

	if payload != nil {
		encodedPayload, err := json.Marshal(payload)
		if err != nil {
			return 0, err
		}
		body = bytes.NewBuffer(encodedPayload)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(response.Body)

	if !slices.Contains(expectedStatusCodes, response.StatusCode) {
		body, _ := io.ReadAll(response.Body)
		return 0, fmt.Errorf("unexpected api response %d %s", response.StatusCode, string(body))
	}

	return response.StatusCode, nil
}
