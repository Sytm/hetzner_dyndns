package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
)

type DynDnsConfig struct {
	HetznerApiKey string
	ZoneName      string
	RecordName    string
	RecordTTL     int
	RecordType    string
}

func main() {
	if len(os.Args) < 2 {
		_, _ = os.Stderr.WriteString("config file not provided\n")
		return
	}
	fmt.Printf("Reading config at %s\n", os.Args[1])
	config, err := readConfig(os.Args[1])
	if err != nil {
		panic(err)
	}

	publicIp, err := getPublicIP(config.RecordType)
	if err != nil {
		panic(err)
	}
	fmt.Printf("Detected public ip address: %s\n", publicIp)

	recordExists, err := checkRecordExists(config)
	if err != nil {
		panic(err)
	}

	if recordExists {
		fmt.Println("Updating existing record")
		err = updateRecord(config, publicIp)
	} else {
		fmt.Println("Creating new record")
		err = createRecord(config, publicIp)
	}

	if err != nil {
		panic(err)
	}
}

func readConfig(configPath string) (*DynDnsConfig, error) {
	configFile, err := os.OpenFile(configPath, os.O_RDONLY, 0600)

	defer func(configFile *os.File) {
		_ = configFile.Close()
	}(configFile)

	if err != nil {
		return nil, err
	}

	decoder := json.NewDecoder(configFile)
	config := DynDnsConfig{}

	err = decoder.Decode(&config)
	if err != nil {
		return nil, err
	}

	if config.RecordType != "AAAA" && config.RecordType != "A" {
		return nil, fmt.Errorf("unknown record type %s", config.RecordType)
	}

	return &config, nil
}

func getPublicIP(recordType string) (string, error) {
	apiUrl := "https://ipv6.seeip.org"
	if recordType == "A" {
		apiUrl = "https://ipv4.seeip.org"
	}

	res, err := http.Get(apiUrl)
	if err != nil {
		return "", err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(res.Body)

	ip, err := io.ReadAll(res.Body)
	if err != nil {
		return "", err
	}

	return string(ip), nil
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

func checkRecordExists(config *DynDnsConfig) (bool, error) {
	endpoint := fmt.Sprintf("https://api.hetzner.cloud/v1/zones/%s/rrsets/%s/%s", config.ZoneName, config.RecordName, config.RecordType)

	statusCode, err := do("GET", config.HetznerApiKey, endpoint, nil, []int{200, 404})

	return statusCode == 200, err
}

func createRecord(config *DynDnsConfig, publicIp string) error {
	endpoint := fmt.Sprintf("https://api.hetzner.cloud/v1/zones/%s/rrsets", config.ZoneName)

	payload := &rrSetPayload{
		Name: config.RecordName,
		Type: config.RecordType,
		TTL:  config.RecordTTL,
		Records: []rrSetRecord{
			{
				Value: publicIp,
			},
		},
	}

	_, err := do("POST", config.HetznerApiKey, endpoint, payload, []int{201})
	return err
}

func updateRecord(config *DynDnsConfig, publicIp string) error {
	endpoint := fmt.Sprintf("https://api.hetzner.cloud/v1/zones/%s/rrsets/%s/%s/actions/set_records", config.ZoneName, config.RecordName, config.RecordType)

	payload := &rrSetPayload{
		Records: []rrSetRecord{
			{
				Value: publicIp,
			},
		},
	}

	_, err := do("POST", config.HetznerApiKey, endpoint, payload, []int{201})
	return err
}

func do(method string, apiKey string, url string, payload *rrSetPayload, expectedStatusCodes []int) (int, error) {
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
