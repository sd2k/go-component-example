package main

import (
	"errors"
	"fmt"
	"net/netip"
	"time"

	monotonicclock "github.com/sd2k/go-component-example/internal/wasi/clocks/monotonic-clock"
	"github.com/sd2k/go-component-example/internal/wasi/io/poll"
	streams "github.com/sd2k/go-component-example/internal/wasi/io/streams"
	instancenetwork "github.com/sd2k/go-component-example/internal/wasi/sockets/instance-network"
	ipnamelookup "github.com/sd2k/go-component-example/internal/wasi/sockets/ip-name-lookup"
	"github.com/sd2k/go-component-example/internal/wasi/sockets/network"
	tcp "github.com/sd2k/go-component-example/internal/wasi/sockets/tcp"
	tcpcreatesocket "github.com/sd2k/go-component-example/internal/wasi/sockets/tcp-create-socket"
	udp "github.com/sd2k/go-component-example/internal/wasi/sockets/udp"
	udpcreatesocket "github.com/sd2k/go-component-example/internal/wasi/sockets/udp-create-socket"
	"go.bytecodealliance.org/cm"
)

// Add fields to track streams for each socket
type wasiNetDev struct {
	nw instancenetwork.Network
	// Maps socket descriptors to their streams
	tcpStreams map[int]tcpStreams
	udpStreams map[int]udpStreams
}

// Holds TCP input and output streams
type tcpStreams struct {
	input  streams.InputStream
	output streams.OutputStream
}

// Holds UDP datagram streams
type udpStreams struct {
	incoming udp.IncomingDatagramStream
	outgoing udp.OutgoingDatagramStream
}

// NewWasiNetDev creates a new WASI network device
func newWasiNetDev(network instancenetwork.Network) *wasiNetDev {
	return &wasiNetDev{
		nw:         network,
		tcpStreams: make(map[int]tcpStreams),
		udpStreams: make(map[int]udpStreams),
	}
}

// Accept implements netdev.Netdever.
func (w *wasiNetDev) Accept(sockfd int) (int, netip.AddrPort, error) {
	tcpSocket := cm.Reinterpret[tcp.TCPSocket](uint32(sockfd))

	// Accept connection
	result, err, isErr := tcpSocket.Accept().Result()
	if isErr {
		return 0, netip.AddrPort{}, fmt.Errorf("failed to accept: %v", err)
	}

	// Get the new socket and streams
	tuple := result
	newSocket := tuple.F0
	inputStream := tuple.F1
	outputStream := tuple.F2

	// Store the streams for later use
	newSockfd := int(cm.Reinterpret[uint32](newSocket))
	w.tcpStreams[newSockfd] = tcpStreams{
		input:  inputStream,
		output: outputStream,
	}

	// Get remote address
	addrResult, err, isErr := newSocket.RemoteAddress().Result()
	if isErr {
		return 0, netip.AddrPort{}, fmt.Errorf("failed to get remote address: %v", err)
	}

	// Convert to netip.AddrPort
	remoteAddr := convertToNetipAddrPort(addrResult)

	return newSockfd, remoteAddr, nil
}

// Addr implements netdev.Netdever.
//
// It returns the IP address assigned to the interface, either by DHCP or statically.
func (w *wasiNetDev) Addr() (netip.Addr, error) {
	// This would typically return the IP address of the network interface
	// Since WASI doesn't have a direct way to get this, we'd need to
	// create a socket, bind to 0.0.0.0, and then get the local address

	// For now, return an unspecified address
	return netip.IPv4Unspecified(), nil
}

// Bind implements netdev.Netdever.
func (w *wasiNetDev) Bind(sockfd int, ip netip.AddrPort) error {
	// Create IP socket address
	addr := createIPSocketAddress(ip)

	// Try TCP first
	tcpSocket := cm.Reinterpret[tcp.TCPSocket](uint32(sockfd))
	_, _, isErr := tcpSocket.StartBind(w.nw, addr).Result()
	if !isErr {
		// It's a TCP socket, finish the bind
		_, _, isErr = tcpSocket.FinishBind().Result()
		if isErr {
			return fmt.Errorf("failed to finish TCP bind")
		}
		return nil
	}

	// Try UDP if TCP failed
	udpSocket := cm.Reinterpret[udp.UDPSocket](uint32(sockfd))
	_, _, isErr = udpSocket.StartBind(w.nw, addr).Result()
	if isErr {
		return fmt.Errorf("failed to start bind")
	}

	_, _, isErr = udpSocket.FinishBind().Result()
	if isErr {
		return fmt.Errorf("failed to finish UDP bind")
	}

	return nil
}

// Close implements netdev.Netdever.
func (w *wasiNetDev) Close(sockfd int) error {
	// Clean up TCP streams if they exist
	if tcpStream, ok := w.tcpStreams[sockfd]; ok {
		tcpStream.input.ResourceDrop()
		tcpStream.output.ResourceDrop()
		delete(w.tcpStreams, sockfd)
	}

	// Clean up UDP streams if they exist
	if udpStream, ok := w.udpStreams[sockfd]; ok {
		udpStream.incoming.ResourceDrop()
		udpStream.outgoing.ResourceDrop()
		delete(w.udpStreams, sockfd)
	}

	// Try to close as TCP socket
	tcpSocket := cm.Reinterpret[tcp.TCPSocket](uint32(sockfd))
	tcpSocket.ResourceDrop()

	// Also try UDP socket (only one will be valid)
	udpSocket := cm.Reinterpret[udp.UDPSocket](uint32(sockfd))
	udpSocket.ResourceDrop()

	return nil
}

// Connect implements netdev.Netdever.
func (w *wasiNetDev) Connect(sockfd int, host string, ip netip.AddrPort) error {
	// Create IP socket address
	addr := createIPSocketAddress(ip)

	// Try TCP first
	tcpSocket := cm.Reinterpret[tcp.TCPSocket](uint32(sockfd))
	_, _, isErr := tcpSocket.StartConnect(w.nw, addr).Result()
	if !isErr {
		// It's a TCP socket, finish the connect
		result, err, isErr := tcpSocket.FinishConnect().Result()
		if isErr {
			return fmt.Errorf("failed to finish TCP connect: %v", err)
		}

		// Store the streams
		tuple := result
		w.tcpStreams[sockfd] = tcpStreams{
			input:  tuple.F0,
			output: tuple.F1,
		}

		return nil
	}

	// UDP doesn't have a traditional connect
	udpSocket := cm.Reinterpret[udp.UDPSocket](uint32(sockfd))
	result, err, isErr := udpSocket.Stream(cm.Some(addr)).Result()
	if isErr {
		return fmt.Errorf("failed to connect UDP socket: %v", err)
	}

	// Store the UDP streams
	tuple := result
	w.udpStreams[sockfd] = udpStreams{
		incoming: tuple.F0,
		outgoing: tuple.F1,
	}

	return nil
}

const TIMEOUT_NS monotonicclock.Duration = 1000000000

func blockUntil(pollable poll.Pollable, timeout poll.Pollable) error {
	pollables := cm.NewList(new(poll.Pollable), 2)
	buf := []poll.Pollable{pollable, timeout}
	copy(pollables.Slice(), buf)
	ready := poll.Poll(pollables)
	if ready.Len() == 0 {
		panic("empty pollables")
	}
	p := ready.Slice()[0]
	if p == 0 {
		return nil
	}
	if p == 1 {
		return errors.New(network.ErrorCodeTimeout.String())
	}
	return errors.New("unreachable")
}

// GetHostByName implements netdev.Netdever.
//
// It returns the IP address of either a hostname or IPv4 address in standard dot notation.
func (w *wasiNetDev) GetHostByName(name string) (netip.Addr, error) {
	stream, err, isErr := ipnamelookup.ResolveAddresses(w.nw, name).Result()
	if isErr {
		return netip.Addr{}, fmt.Errorf("error resolving addresses: %v", err)
	}

	timeout := monotonicclock.SubscribeDuration(TIMEOUT_NS)
	pollable := stream.Subscribe()

	for {
		addrOpt, err, isErr := stream.ResolveNextAddress().Result()
		if isErr {
			if err == network.ErrorCodeWouldBlock {
				if err := blockUntil(pollable, timeout); err != nil {
					return netip.Addr{}, err
				}
				continue
			}
			return netip.Addr{}, fmt.Errorf("error resolving: %s", err)
		}
		addr := addrOpt.Some()
		if addr == nil {
			return netip.Addr{}, errors.New(network.ErrorCodeNameUnresolvable.String())
		}
		if v4 := addr.IPv4(); v4 != nil {
			return netip.AddrFrom4(*v4), nil
		}
		if v6 := addr.IPv6(); v6 != nil {
			v6As16 := [16]byte{}
			for i := range 8 {
				v6As16[i*2] = byte(v6[i] >> 8)
				v6As16[i*2+1] = byte(v6[i])
			}
			return netip.AddrFrom16(v6As16), nil
		}
	}
}

// Listen implements netdev.Netdever.
func (w *wasiNetDev) Listen(sockfd int, backlog int) error {
	// Only TCP sockets can listen
	tcpSocket := cm.Reinterpret[tcp.TCPSocket](uint32(sockfd))

	// Set backlog size
	_, _, isErr := tcpSocket.SetListenBacklogSize(uint64(backlog)).Result()
	if isErr {
		return fmt.Errorf("failed to set listen backlog")
	}

	// Start listening
	_, _, isErr = tcpSocket.StartListen().Result()
	if isErr {
		return fmt.Errorf("failed to start listen")
	}

	// Finish listening
	_, _, isErr = tcpSocket.FinishListen().Result()
	if isErr {
		return fmt.Errorf("failed to finish listen")
	}

	return nil
}

// Recv implements netdev.Netdever.
func (w *wasiNetDev) Recv(sockfd int, buf []byte, flags int, deadline time.Time) (int, error) {
	// Check if we have TCP streams for this socket
	if tcpStream, ok := w.tcpStreams[sockfd]; ok {
		// Use the TCP input stream
		inputStream := tcpStream.input

		// Read from the stream
		result, err, isErr := inputStream.Read(uint64(len(buf))).Result()
		if isErr {
			return 0, fmt.Errorf("failed to read from TCP stream: %v", err)
		}

		// Copy data to the buffer
		n := copy(buf, result.Slice())
		return n, nil
	}

	// Check if we have UDP streams for this socket
	if udpStream, ok := w.udpStreams[sockfd]; ok {
		// Use the UDP incoming datagram stream
		incomingStream := udpStream.incoming

		// Receive datagrams
		result, err, isErr := incomingStream.Receive(1).Result() // Get at most 1 datagram
		if isErr {
			return 0, fmt.Errorf("failed to receive UDP datagram: %v", err)
		}

		// Get the datagrams
		datagrams := result.Slice()
		if len(datagrams) == 0 {
			return 0, nil // No data available
		}

		// Copy data from the first datagram
		datagram := datagrams[0]
		n := copy(buf, datagram.Data.Slice())
		return n, nil
	}

	return 0, fmt.Errorf("no stream found for socket %d", sockfd)
}

// Send implements netdev.Netdever.
func (w *wasiNetDev) Send(sockfd int, buf []byte, flags int, deadline time.Time) (int, error) {
	// Check if we have TCP streams for this socket
	if tcpStream, ok := w.tcpStreams[sockfd]; ok {
		// Use the TCP output stream
		outputStream := tcpStream.output

		// Convert buf to a cm.List
		list := cm.NewList[uint8](nil, uint32(len(buf)))
		copy(list.Slice(), buf)

		// Write to the stream
		_, err, isErr := outputStream.Write(list).Result()
		if isErr {
			return 0, fmt.Errorf("failed to write to TCP stream: %v", err)
		}

		// Return the length of data we sent
		return len(buf), nil
	}

	// For UDP streams
	if udpStream, ok := w.udpStreams[sockfd]; ok {
		outgoingStream := udpStream.outgoing

		// Create a datagram
		data := cm.NewList[uint8](nil, uint32(len(buf)))
		copy(data.Slice(), buf)

		datagram := udp.OutgoingDatagram{
			Data:          data,
			RemoteAddress: cm.None[network.IPSocketAddress](),
		}

		// Create a list with one datagram
		datagrams := cm.NewList[udp.OutgoingDatagram](nil, 1)
		datagrams.Slice()[0] = datagram

		_, err, isErr := outgoingStream.Send(datagrams).Result()
		if isErr {
			return 0, fmt.Errorf("failed to send UDP datagram: %v", err)
		}

		// Return the length of data we sent
		return len(buf), nil
	}

	return 0, fmt.Errorf("no stream found for socket %d", sockfd)
}

// SetSockOpt implements netdev.Netdever.
func (w *wasiNetDev) SetSockOpt(sockfd int, level int, opt int, value interface{}) error {
	// Map common socket options to WASI equivalents
	tcpSocket := cm.Reinterpret[tcp.TCPSocket](uint32(sockfd))

	// Handle common options
	switch opt {
	case 8: // SO_KEEPALIVE
		if boolVal, ok := value.(bool); ok {
			_, _, isErr := tcpSocket.SetKeepAliveEnabled(boolVal).Result()
			if !isErr {
				return nil
			}
		}
	}

	// Many options don't have direct WASI equivalents
	return nil
}

// Socket implements netdev.Netdever.
func (w *wasiNetDev) Socket(domain int, stype int, protocol int) (int, error) {
	// Map domain to WASI IP address family
	var family network.IPAddressFamily
	if domain == 2 { // AF_INET
		family = network.IPAddressFamilyIPv4
	} else if domain == 10 { // AF_INET6
		family = network.IPAddressFamilyIPv6
	} else {
		return 0, fmt.Errorf("unsupported address family: %d", domain)
	}

	// Create the appropriate socket type
	if stype == 1 { // SOCK_STREAM
		// Use TCP socket
		result := tcpcreatesocket.CreateTCPSocket(family)
		tcpSocket, err, isErr := result.Result()
		if isErr {
			return 0, fmt.Errorf("failed to create TCP socket: %v", err)
		}
		return int(cm.Reinterpret[uint32](tcpSocket)), nil
	} else if stype == 2 { // SOCK_DGRAM
		// Use UDP socket
		result := udpcreatesocket.CreateUDPSocket(family)
		udpSocket, err, isErr := result.Result()
		if isErr {
			return 0, fmt.Errorf("failed to create UDP socket: %v", err)
		}
		return int(cm.Reinterpret[uint32](udpSocket)), nil
	} else {
		return 0, fmt.Errorf("unsupported socket type: %d", stype)
	}
}

// Helper functions

// convertToNetipAddrPort converts a WASI IP socket address to a netip.AddrPort
func convertToNetipAddrPort(addr network.IPSocketAddress) netip.AddrPort {
	switch addr.Tag() {
	case 0: // IPv4
		ipv4Addr := cm.Case[network.IPv4SocketAddress](&addr, 0)
		port := ipv4Addr.Port
		ip := netip.AddrFrom4([4]byte{
			byte(ipv4Addr.Address[0]),
			byte(ipv4Addr.Address[1]),
			byte(ipv4Addr.Address[2]),
			byte(ipv4Addr.Address[3]),
		})
		return netip.AddrPortFrom(ip, uint16(port))
	case 1: // IPv6
		ipv6Addr := cm.Case[network.IPv6SocketAddress](&addr, 1)
		port := ipv6Addr.Port
		// Convert from array of uint16 to [16]byte
		var bytes [16]byte
		for i, v := range ipv6Addr.Address {
			bytes[i*2] = byte(v >> 8)
			bytes[i*2+1] = byte(v)
		}
		ip := netip.AddrFrom16(bytes)
		return netip.AddrPortFrom(ip, uint16(port))
	default:
		return netip.AddrPort{}
	}
}

// createIPSocketAddress converts a netip.AddrPort to a WASI IP socket address
func createIPSocketAddress(addr netip.AddrPort) network.IPSocketAddress {
	if addr.Addr().Is4() {
		// Create IPv4 socket address
		bytes := addr.Addr().As4()
		v4addr := network.IPv4SocketAddress{
			Port: uint16(addr.Port()),
			Address: network.IPv4Address{
				bytes[0], bytes[1], bytes[2], bytes[3],
			},
		}
		return network.IPSocketAddressIPv4(v4addr)
	} else {
		// Create IPv6 socket address
		bytes := addr.Addr().As16()
		var addr6 [8]uint16
		for i := 0; i < 8; i++ {
			addr6[i] = uint16(bytes[i*2])<<8 | uint16(bytes[i*2+1])
		}
		v6addr := network.IPv6SocketAddress{
			Port:     uint16(addr.Port()),
			FlowInfo: 0,
			Address:  network.IPv6Address(addr6),
			ScopeID:  0,
		}
		return network.IPSocketAddressIPv6(v6addr)
	}
}
