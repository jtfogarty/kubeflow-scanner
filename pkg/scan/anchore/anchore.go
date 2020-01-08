package anchore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	log "github.com/sirupsen/logrus"
)

// Implementation Notes

// Anchore Scanning Workflow
// Step 1. Add new image to Anchore - ScanImage() function
// Step 2. Wait for automatic analysis
// Step 3. Poll service for Analysis Status - GetImage() function
// Step 4. Fetch all Critical and High Vulnerabilities - GetVuln() function

// Reference code: https://github.com/banzaicloud/anchore-image-validator/blob/master/pkg/anchore/client.go

// Client holds the information needed to authenticate to a service endpoint
type Client struct {
	Username   string
	Password   string
	ServiceURL string
	Account    string
}

// Image type for Anchore image
type Image struct {
	ImageDigest string `json:"imageDigest"`
	LastUpdated string `json:"last_updated"`
}

func getAnchoreClient() (*Client, error) {
	client := &Client{
		Username:   os.Getenv("ANCHORE_CLI_USER"),
		Password:   os.Getenv("ANCHORE_CLI_PASS"),
		ServiceURL: os.Getenv("ANCHORE_CLI_URL"),
		Account:    os.Getenv("ANCHORE_ACCOUNT"),
	}
	// TODO(swiftdiaries):: check if any env vars are empty and error out
	return client, nil
}

func anchoreRequest(ctx context.Context, path string, method string, bodyParams map[string]string) ([]byte, error) {
	anchoreClient, err := getAnchoreClient()
	if err != nil {
		return nil, err
	}

	anchoreURL := anchoreClient.ServiceURL + path

	jsonRequestData, err := json.Marshal(bodyParams)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, method, anchoreURL, bytes.NewBuffer(jsonRequestData))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(anchoreClient.Username, anchoreClient.Password)
	req.Header.Add("Content-Type", "application/json")
	if anchoreClient.Account != "" {
		req.Header.Add("x-anchore-account", anchoreClient.Account)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyText, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("anchore request returned a non-zero error: %s", bodyText)
	}
	log.WithFields(log.Fields{
		"Response": fmt.Sprintf("%s", bodyText),
	}).Info("Anchore Response Body")

	return bodyText, nil
}

func getImageDigest(ctx context.Context, imageName string) (string, error) {
	params := map[string]string{"tag": imageName}
	body, err := anchoreRequest(ctx, "/images?history=false", "GET", params)
	if err != nil {
		return "", err
	}
	var images []Image
	err = json.Unmarshal(body, &images)
	if err != nil {
		return "", err
	}
	return images[0].ImageDigest, nil
}

// ScanImage sends a POST request to the Anchore Engine to start scanning for vulnerabilities
// needs the ANCHORE credentials and the service URL to authenticate
// Step 1 in the Anchore Scanning Workflow
func ScanImage(ctx context.Context, imageName string) error {
	params := map[string]string{"tag": imageName}
	_, err := anchoreRequest(ctx, "/images?force=true&autosubscribe=false", "POST", params)
	if err != nil {
		return err
	}

	log.WithFields(log.Fields{
		"Image": imageName,
	}).Info("Added image to be scanned")

	return nil
}

// GetImage fetches the image from the Anchore Database and gets the analysis status
// needs the Anchore credentials and the service URL to authenticate
// Step 3 in Anchore scanning workflow, used to poll the Anchore service and get analysis status
func GetImage(ctx context.Context, imageName string) error {
	digest, err := getImageDigest(ctx, imageName)
	if err != nil {
		return err
	}
	log.Infof("Image Digest: %s", digest)
	params := map[string]string{"digest": digest, "tag": imageName}
	_, err = anchoreRequest(ctx, "/images", "GET", params)
	if err != nil {
		return err
	}

	log.WithFields(log.Fields{
		"Image": imageName,
	}).Info("Image Scan Response")

	return nil
}

// GetVuln fetches all the vulnerabilties for an image that has completed scanning analysis
// Step 4 and final step in Anchore scanning workflow, once GetImage indicates a completed scan
// GetVuln is called
func GetVuln(ctx context.Context, imageName string) error {
	digest, err := getImageDigest(ctx, imageName)
	if err != nil {
		return err
	}
	log.Infof("Image Digest: %s", digest)
	requestPath := "/images/" + digest + "/vuln/all"
	_, err = anchoreRequest(ctx, requestPath, "GET", nil)
	if err != nil {
		return err
	}
	return nil
}
