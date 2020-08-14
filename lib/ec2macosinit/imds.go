package ec2macosinit

import (
	"fmt"
	"net/http"
	"strconv"
)

const (
	imdsBase              = "http://169.254.169.254/latest/"
	imdsTokenTTL          = 21600
	tokenEndpoint         = "api/token"
	tokenRequestTTLHeader = "X-aws-ec2-metadata-token-ttl-seconds"
	tokenHeader           = "X-aws-ec2-metadata-token"
)

// IMDS config contains the current instance ID and a place for the IMDSv2 token to be stored.
// Using IMDSv2:
// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/configuring-instance-metadata-service.html#instance-metadata-v2-how-it-works
type IMDSConfig struct {
	token      string
	InstanceID string
}

// getIMDSProperty gets a given endpoint property from IMDS.
func (i *IMDSConfig) getIMDSProperty(endpoint string) (value string, httpResponseCode int, err error) {
	// Check that an IMDSv2 token exists - get one if it doesn't
	if i.token == "" {
		err = i.getNewToken()
		if err != nil {
			return "", 0, fmt.Errorf("ec2macosinit: error while getting new IMDS token: %s\n", err)
		}
	}

	// Create request
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodGet, imdsBase+endpoint, nil)
	if err != nil {
		return "", 0, fmt.Errorf("ec2macosinit: error while creating new HTTP request: %s\n", err)
	}
	req.Header.Set(tokenHeader, i.token) // set IMDSv2 token

	// Make request
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("ec2macosinit: error while requesting IMDS property: %s\n", err)
	}

	// Convert returned io.ReadCloser to string
	value, err = ioReadCloserToString(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("ec2macosinit: error reading response body: %s\n", err)
	}

	return value, resp.StatusCode, nil
}

// getNewToken gets a new IMDSv2 token from the IMDS API.
func (i *IMDSConfig) getNewToken() (err error) {
	// Create request
	client := &http.Client{}
	req, err := http.NewRequest(http.MethodPut, imdsBase+tokenEndpoint, nil)
	if err != nil {
		return fmt.Errorf("ec2macosinit: error while creating new HTTP request: %s\n", err)
	}
	req.Header.Set(tokenRequestTTLHeader, strconv.FormatInt(int64(imdsTokenTTL), 10))

	// Make request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ec2macosinit: error while requesting new token: %s\n", err)
	}

	// Validate response code
	if resp.StatusCode != 200 {
		return fmt.Errorf("ec2macosinit: received a non-200 status code from IMDS: %d - %s\n",
			resp.StatusCode,
			resp.Status,
		)
	}

	// Set returned value
	i.token, err = ioReadCloserToString(resp.Body)
	if err != nil {
		return fmt.Errorf("ec2macosinit: error reading response body: %s\n", err)
	}

	return nil
}

// UpdateInstanceID is a wrapper for getIMDSProperty that gets the current instance ID for the attached config.
func (i *IMDSConfig) UpdateInstanceID() (err error) {
	// If instance ID is already set, this doesn't need to be run
	if i.InstanceID != "" {
		return nil
	}

	// Get IMDS property "meta-data/instance-id"
	i.InstanceID, _, err = i.getIMDSProperty("meta-data/instance-id")
	if err != nil {
		return fmt.Errorf("ec2macosinit: error getting instance ID from IMDS: %s\n", err)
	}

	// Validate that an ID was returned
	if i.InstanceID == "" {
		return fmt.Errorf("ec2macosinit: an empty instance ID was returned from IMDS\n")
	}

	return nil
}
