package ec2macosinit

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/digineo/go-ping"
)

const (
	pingCountDefault = 3
	pingPayloadSize  = 56
)

// NetworkCheckModule contains contains all necessary configuration fields for running a NetworkCheck module.
type NetworkCheckModule struct {
	PingCount int `toml:"PingCount"`
}

// Do for NetworkCheck Module gets the default gateway and pings it to check if the network is up.
func (c *NetworkCheckModule) Do() (message string, err error) {
	// Get default gateway
	out, err := executeCommand([]string{"/bin/zsh", "-c", "route -n get default | grep gateway"}, "", []string{})
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: error while running route command to get default gateway with stderr [%s]: %s\n", out.stderr, err)
	}
	gatewayFields := strings.Fields(out.stdout)
	if len(gatewayFields) != 2 {
		return "", fmt.Errorf("ec2macosinit: unexpected output from route command: %s\n", out.stdout)
	}

	// Resolve IP address
	defaultGatewayIP, err := net.ResolveIPAddr("ip4", gatewayFields[1])
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: error resolving default gateway IP address: %s\n", err)
	}

	// Ping default gateway
	pinger, err := ping.New("0.0.0.0", "")
	if err != nil {
		return "", fmt.Errorf("ec2macosinit: error setting up new pinger: %s\n", err)
	}
	// If PingCount is unset, default to 3
	if c.PingCount == 0 {
		c.PingCount = pingCountDefault
	}
	pinger.SetPayloadSize(pingPayloadSize)
	rtt, err := pinger.PingAttempts(defaultGatewayIP, time.Second, int(c.PingCount))
	if err != nil {
		// If network is not up, this will error with an i/o timeout
		return "", fmt.Errorf("ec2macosinit: error pinging default gateway: %s\n", err)
	}

	return fmt.Sprintf("successfully pinged default gateway with a RTT of %v", rtt), nil
}
