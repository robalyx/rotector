package ip

import (
	"net"

	"github.com/rotector/rotector/internal/common/config"
	"go.uber.org/zap"
)

// Checker validates IP addresses against known private and special-use ranges.
type Checker struct {
	privateCIDRs []*net.IPNet // List of private and special-use CIDR ranges
	logger       *zap.Logger
	config       *config.RPCIPConfig
}

// NewChecker creates a new Checker with predefined private ranges.
func NewChecker(logger *zap.Logger, config *config.RPCIPConfig) *Checker {
	// Define private network ranges in CIDR notation
	cidrs := [...]string{
		// IPv4 ranges
		"0.0.0.0/8",          // RFC 1122 - "This" network
		"10.0.0.0/8",         // RFC 1918 - Private network (24-bit block)
		"100.64.0.0/10",      // RFC 6598 - Carrier-grade NAT
		"127.0.0.0/8",        // RFC 1122 - Localhost
		"169.254.0.0/16",     // RFC 3927 - Link-local
		"172.16.0.0/12",      // RFC 1918 - Private network (20-bit block)
		"192.0.0.0/24",       // RFC 5736 - IANA IPv4 Special Purpose Address Registry
		"192.0.2.0/24",       // RFC 5737 - TEST-NET-1
		"192.88.99.0/24",     // RFC 3068 - Formerly used for IPv6 to IPv4 relay
		"192.168.0.0/16",     // RFC 1918 - Private network (16-bit block)
		"198.18.0.0/15",      // RFC 2544 - Benchmarking
		"198.51.100.0/24",    // RFC 5737 - TEST-NET-2
		"203.0.113.0/24",     // RFC 5737 - TEST-NET-3
		"224.0.0.0/4",        // RFC 3171 - Multicast
		"233.252.0.0/24",     // RFC 5771 - MCAST-TEST-NET
		"240.0.0.0/4",        // RFC 1112 - Reserved for future use
		"255.255.255.255/32", // RFC 919 - Limited broadcast

		// IPv6 ranges
		"::1/128",       // Localhost IPv6
		"fc00::/7",      // Unique local address IPv6
		"fe80::/10",     // Link local address IPv6
		"ff00::/8",      // IPv6 multicast
		"2001:db8::/32", // IPv6 documentation prefix
	}

	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, ipNet, _ := net.ParseCIDR(cidr)
		nets = append(nets, ipNet)
	}

	return &Checker{
		privateCIDRs: nets,
		logger:       logger,
		config:       config,
	}
}

// ValidateIP checks if a string IP is valid and usable.
func (c *Checker) ValidateIP(ip string) string {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return UnknownIP
	}

	if c.IsValidPublicIP(parsedIP) {
		return ip
	}
	return UnknownIP
}

// IsValidPublicIP checks if an IP is valid for use.
func (c *Checker) IsValidPublicIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	// In development mode, allow local IPs
	if c.config.AllowLocalIPs {
		return true
	}

	// In production, require public IPs
	if !ip.IsGlobalUnicast() {
		return false
	}
	return !c.isPrivateSubnet(ip)
}

// IsTrustedProxy checks if an IP is in the trusted proxy list.
func (c *Checker) IsTrustedProxy(ip net.IP) bool {
	trustedProxies := c.parseCIDRs(c.config.TrustedProxies)
	for _, proxy := range trustedProxies {
		if proxy.Contains(ip) {
			return true
		}
	}
	return false
}

// parseCIDRs parses a list of CIDR strings into IPNets.
// It returns the valid IPNets and logs any parsing errors.
func (c *Checker) parseCIDRs(cidrs []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, cidr := range cidrs {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			c.logger.Error("Invalid CIDR",
				zap.String("cidr", cidr),
				zap.Error(err))
			continue
		}
		nets = append(nets, ipNet)
	}
	return nets
}

// isPrivateSubnet checks if an IP address belongs to a private or special-use range.
func (c *Checker) isPrivateSubnet(ipAddress net.IP) bool {
	for _, net := range c.privateCIDRs {
		if net.Contains(ipAddress) {
			return true
		}
	}
	return false
}
