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
	parsedIp := net.ParseIP(ipString)
	if parsedIp == nil || ((recordType == "A") == (parsedIp.To4() == nil)) {
		log.Fatalf("service returned invalid ip address %s", ipString)
	}

	if currentAddress := getCurrentRecord(config, recordType); currentAddress == "" {
		createRecord(config, recordType, ipString)
	} else {
		if parsedIp.Equal(net.ParseIP(currentAddress)) {
			log.Println("Skipping update because address is already up-to-date")
		} else {
			updateRecord(config, recordType, ipString)
		}
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

type rrSetResponse struct {
	RRSet rrSetPayload `json:"rrset"`
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

func getCurrentRecord(config *DynDnsConfig, recordType string) string {
	endpoint := fmt.Sprintf("https://api.hetzner.cloud/v1/zones/%s/rrsets/%s/%s", config.ZoneName, config.RecordName, recordType)

	statusCode, body, err := doAuthenticated("GET", config.HetznerApiKey, endpoint, nil, []int{200, 404}, true)

	if err != nil {
		log.Fatalln("could not check record existence", err)
	} else if statusCode == 404 {
		return ""
	}

	parsedResponse := rrSetResponse{}
	err = json.Unmarshal(body, &parsedResponse)
	if err != nil {
		log.Fatalf("could not parse api response %s %v\n", body, err)
	}

	for _, record := range parsedResponse.RRSet.Records {
		return record.Value
	}

	return ""
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

	_, _, err := doAuthenticated("POST", config.HetznerApiKey, endpoint, payload, []int{201}, false)

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

	_, _, err := doAuthenticated("POST", config.HetznerApiKey, endpoint, payload, []int{201}, false)

	if err != nil {
		log.Fatalf("could not update record of type %s to %s %v", recordType, publicIp, err)
	}
}

func doAuthenticated(method string, apiKey string, url string, payload *rrSetPayload, expectedStatusCodes []int, readBody bool) (int, []byte, error) {
	var body io.Reader = http.NoBody

	if payload != nil {
		encodedPayload, err := json.Marshal(payload)
		if err != nil {
			return 0, nil, err
		}
		body = bytes.NewBuffer(encodedPayload)
	}

	req, err := http.NewRequest(method, url, body)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func(Body io.ReadCloser) {
		err := Body.Close()
		if err != nil {
			log.Println("could not properly close response body", err)
		}
	}(response.Body)

	if !slices.Contains(expectedStatusCodes, response.StatusCode) {
		responseBody, _ := io.ReadAll(response.Body)
		return 0, nil, fmt.Errorf("unexpected api response %d %s", response.StatusCode, string(responseBody))
	}
	if readBody {
		responseBody, err := io.ReadAll(response.Body)
		if err != nil {
			return 0, nil, err
		}
		return response.StatusCode, responseBody, nil
	}

	return response.StatusCode, nil, nil
}
