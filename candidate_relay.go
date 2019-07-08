package ice

import (
	"errors"
	"io"
	"net"

	"github.com/pion/turnc"
)

// CandidateRelay ...
type CandidateRelay struct {
	candidateBase

	allocation *turnc.Allocation
	client     io.Closer
	channelMap map[string]*turnc.Channel
}

// CandidateRelayConfig is the config required to create a new CandidateRelay
type CandidateRelayConfig struct {
	CandidateID string
	Network     string
	Address     string
	Port        int
	Component   uint16
	RelAddr     string
	RelPort     int
}

// NewCandidateRelay creates a new relay candidate
func NewCandidateRelay(config *CandidateRelayConfig) (*CandidateRelay, error) {
	candidateID := config.CandidateID

	if candidateID == "" {
		var err error
		candidateID, err = generateCandidateID()
		if err != nil {
			return nil, err
		}
	}

	ip := net.ParseIP(config.Address)
	if ip == nil {
		return nil, ErrAddressParseFailed
	}

	networkType, err := determineNetworkType(config.Network, ip)
	if err != nil {
		return nil, err
	}

	return &CandidateRelay{
		candidateBase: candidateBase{
			id:            candidateID,
			networkType:   networkType,
			candidateType: CandidateTypeRelay,
			address:       config.Address,
			port:          config.Port,
			resolvedAddr:  &net.UDPAddr{IP: ip, Port: config.Port},
			component:     config.Component,
			relatedAddress: &CandidateRelatedAddress{
				Address: config.RelAddr,
				Port:    config.RelPort,
			},
		},
		channelMap: map[string]*turnc.Channel{},
	}, nil
}

func (c *CandidateRelay) setAllocation(client io.Closer, a *turnc.Allocation) {
	c.allocation = a
	c.client = client
}

func (c *CandidateRelay) start(a *Agent, conn net.PacketConn) {
	c.currAgent = a
}

func (c *CandidateRelay) close() error {
	c.lock.Lock()
	defer c.lock.Unlock()
	for _, p := range c.channelMap {
		if err := p.Close(); err != nil {
			return err
		}
	}
	if c.client == nil {
		return nil
	}
	return c.client.Close()
}

func (c *CandidateRelay) addChannel(dst Candidate) error {
	ch, err := c.allocation.Create(dst.addr())
	if err != nil {
		return err
	}

	c.lock.Lock()
	c.channelMap[dst.String()] = ch
	if err = c.channelMap[dst.String()].Bind(); err != nil {
		c.agent().log.Warnf("Failed to Create ChannelBind for %v: %v", dst.String, err)
	}
	c.lock.Unlock()

	go func(remoteAddr net.Addr) {
		log := c.agent().log
		buffer := make([]byte, receiveMTU)
		for {
			n, err := ch.Read(buffer)
			if err != nil {
				return
			}

			log.Debugf("candidate relay received %d bytes", n)

			handleInboundCandidateMsg(c, buffer[:n], remoteAddr, log)
		}
	}(dst.addr())
	return nil
}

func (c *CandidateRelay) writeTo(raw []byte, dst Candidate) (int, error) {
	ch, ok := c.channelMap[dst.String()]
	if !ok {
		return 0, errors.New("no channel created for remote candidate")
	}

	return ch.Write(raw)
}
