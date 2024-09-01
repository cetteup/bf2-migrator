package gamespy

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/dogclan/dumbspy/pkg/gamespy"
	"go.uber.org/multierr"
)

type Provider string

const (
	ProviderBF2Hub  Provider = "bf2hub.com"
	ProviderPlayBF2 Provider = "playbf2.ru"
	ProviderOpenSpy Provider = "openspy.net"

	network     = "tcp4"
	serviceGPCM = "gpcm"
	serviceGPSP = "gpsp"
	portGPCM    = "29900"
	portGPSP    = "29901"

	namespaceID = "12"
	gameName    = "battlefield2"
	productID   = "10493"
)

type Client struct {
	timeout time.Duration
}

func NewClient(timeout int) *Client {
	return &Client{
		timeout: time.Duration(timeout) * time.Second,
	}
}

func (c *Client) GetNicks(provider Provider, email, password string) ([]NickDTO, error) {
	conn, err := connect(getHostname(provider, serviceGPSP), portGPSP)
	if err != nil {
		return nil, err
	}
	defer func() {
		err = multierr.Append(err, disconnect(conn))
	}()

	req := new(gamespy.Packet)
	req.Add("nicks", "")
	req.Add("email", email)
	req.Add("pass", password)
	req.Add("passenc", gamespy.EncodePassword(password))
	req.Add("namespaceid", namespaceID)
	req.Add("gamename", gameName)

	if err = write(conn, c.timeout, req); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	res, err := read(conn, c.timeout)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if errmsg, exists := res.Lookup("errmsg"); exists {
		return nil, fmt.Errorf("%s (code: %s)", errmsg, res.Get("err"))
	}

	var nicks []NickDTO
	current := NickDTO{}
	keys := make(map[string]struct{})
	res.Do(func(element gamespy.KeyValuePair) {
		// Start building a new result when we reach a key we saw before
		_, seen := keys[element.Key]
		if seen {
			nicks = append(nicks, current)
			current = NickDTO{}
			keys = make(map[string]struct{}, len(keys))
		}

		switch element.Key {
		case "nick":
			current.Nick = element.Value
		case "uniquenick":
			current.UniqueNick = element.Value
		default:
			// Skip irrelevant keys
			return
		}

		keys[element.Key] = struct{}{}
	})

	// Add current result if we found (some) keys, but never found another nick
	// (we only "flush" current to nicks on the n+1st result)
	if len(keys) != 0 {
		nicks = append(nicks, current)
	}

	return nicks, nil
}

func (c *Client) CreateUser(provider Provider, email, password, nick string) (err error) {
	conn, err := connect(getHostname(provider, serviceGPCM), portGPCM)
	if err != nil {
		return err
	}
	defer func() {
		err = multierr.Append(err, disconnect(conn))
	}()

	// Read login challenge prompt first, as it is sent immediately upon connecting
	// TODO Login later to verify?
	_, err = read(conn, c.timeout)
	if err != nil {
		return fmt.Errorf("failed to read login challenge prompt: %w", err)
	}

	signup := new(gamespy.Packet)
	signup.Add("newuser", "")
	signup.Add("email", email)
	signup.Add("nick", nick)
	signup.Add("passwordenc", gamespy.EncodePassword(password))
	signup.Add("productid", productID)
	signup.Add("gamename", gameName)
	signup.Add("namespaceid", namespaceID)
	signup.Add("uniquenick", nick)
	signup.Add("id", "1")

	if err = write(conn, c.timeout, signup); err != nil {
		return fmt.Errorf("failed to write request: %w", err)
	}

	res, err := read(conn, c.timeout)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if errmsg, exists := res.Lookup("errmsg"); exists {
		return fmt.Errorf("%s (code: %s)", errmsg, res.Get("err"))
	}

	return nil
}

func connect(host string, port string) (net.Conn, error) {
	raddr, err := net.ResolveTCPAddr(network, net.JoinHostPort(host, port))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve address: %w", err)
	}

	conn, err := net.DialTCP(raddr.Network(), nil, raddr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to %s: %w", raddr.String(), err)
	}

	return conn, nil
}

func disconnect(conn net.Conn) error {
	if conn == nil {
		return nil
	}

	if err := conn.Close(); err != nil {
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		return fmt.Errorf("failed to close connection to %s: %w", conn.RemoteAddr().String(), err)
	}

	return nil
}

func write(conn net.Conn, timeout time.Duration, packet *gamespy.Packet) error {
	if err := conn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
		return fmt.Errorf("failed to set write deadline: %w", err)
	}

	if _, err := conn.Write(packet.Bytes()); err != nil {
		return fmt.Errorf("failed to write packet: %w", err)
	}
	return nil
}

func read(conn net.Conn, timeout time.Duration) (*gamespy.Packet, error) {
	if err := conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return nil, fmt.Errorf("failed to set read deadline: %w", err)
	}

	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	if err != nil {
		return nil, fmt.Errorf("failed to read packet: %w", err)
	}

	res, err := gamespy.NewPacketFromBytes(buffer[:n])
	if err != nil {
		return nil, fmt.Errorf("failed to parse packet: %w", err)
	}
	return res, nil
}

func getHostname(provider Provider, service string) string {
	return service + "." + string(provider)
}
