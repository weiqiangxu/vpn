package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const (
	CmdMsg int32 = iota + 1
	CmdExit
)

const (
	ServerVersion = 1.0
	ClientVersion = 1.0
	ServerAddr    = ":51000"
)

type ExceptionHandler func(ctx *Context, err error)
type ClosedHandler func(ctx *Context)
type HandshakeDoneHandler func(ctx *Context)

// Handler HandshakeHandler
type Handler interface {
	Handshake(conn io.ReadWriteCloser) error
}

type VersionServerHandler struct {
	Version float64
}

// Status HandshakeStatus
type Status byte

const (
	UnknownError    Status = 0
	Success         Status = 1
	VersionNotMatch Status = 2
)

func NewError(status Status, message string) error {
	return errors.New(fmt.Sprintf("Handshake Handler Status is %d, Message: %v", int(status), message))
}

func respHandshakeStatus(status Status, conn io.ReadWriteCloser) error {
	data := [1]byte{byte(status)}
	_, err := conn.Write(data[:])
	if status == Success && err == nil {
		return nil
	}
	// close conn
	_ = conn.Close()
	if err != nil {
		return NewError(status, err.Error())
	}
	return NewError(status, "")
}

func (t *VersionClientHandler) Handshake(conn io.ReadWriteCloser) error {
	// send version float64
	err := binary.Write(conn, binary.BigEndian, t.Version)
	if err != nil {
		return NewError(UnknownError, err.Error())
	}

	// read handshake resp (1byte)
	buf := make([]byte, 1)
	n, err := conn.Read(buf)
	if err != nil {
		return NewError(UnknownError, err.Error())
	}
	status := Status(buf[:n][0])
	if status != Success {
		return NewError(status, "")
	}
	return nil
}

func (t *VersionServerHandler) Handshake(conn io.ReadWriteCloser) error {
	var version float64
	err := binary.Read(conn, binary.BigEndian, &version)
	if err != nil {
		return respHandshakeStatus(UnknownError, conn)
	}
	// var status Status
	var status Status
	if version != t.Version {
		status = VersionNotMatch
	} else {
		status = Success
	}
	return respHandshakeStatus(status, conn)
}

type Reader struct {
	cache     []byte
	reader    io.Reader
	bufLength int
}

const HeaderLength int32 = 4

func (c *Reader) Read() (*Packet, error) {
	isHeader := true
	var bodyLength int32
	buf := make([]byte, 16)
	for {
		n, err := c.reader.Read(buf)
		if err != nil {
			return nil, err
		}
		// reset cache and append cache to data
		data := append(c.Reset(), buf[:n]...)
		length := int32(len(data))

		for {
			if isHeader {
				// 解析 bodyLength
				if length < HeaderLength {
					// 读取到的数据长度不足 HeaderLength 长度
					// 记录缓存，break 进入下一次读取
					c.cache = append(c.cache, data...)
					break
				} else if length == HeaderLength {
					// 数据长度正好等于 HeaderLength 长度
					// 解析 bodyLength 完成，break 进入下一次读取
					bodyLength = Byte2Int(data[:HeaderLength])
					isHeader = false
					break
				} else {
					// 数据长度大于 HeaderLength
					// 解析 bodyLength 并将剩余的数据放到 data 中后继续解析 body
					bodyLength = Byte2Int(data[:length])
					isHeader = false
					data = data[HeaderLength:]
					length = int32(len(data))
				}
			} else {
				// 解析 body
				if length < bodyLength {
					// 读取到的数据长度不足 body 长度
					// 记录缓存，break 进入下一次读取
					c.cache = append(c.cache, data...)
					break
				} else {
					// 数据长度大于等于 bodyLength 长度
					// 解析 body，返回数据 packet，将剩余的数据放到 cache 中
					packet := UnmarshalBody(data[:bodyLength])
					c.cache = append(c.cache, data[bodyLength:]...)
					return packet, nil
				}
			}
		}
	}
}

type Packet struct {
	Cmd           int32
	TransactionId int32
	Data          []byte
}

// UnmarshalBody decode []byte to Packet
func UnmarshalBody(buf []byte) *Packet {
	reader := bytes.NewReader(buf)

	var cmd, transactionId int32
	// read int32(4byte) Cmd And TransactionId
	_ = binary.Read(reader, binary.BigEndian, &cmd)
	_ = binary.Read(reader, binary.BigEndian, &transactionId)

	// read other bytes
	data := make([]byte, reader.Len())
	n, _ := reader.Read(data)
	return &Packet{
		Cmd:           cmd,
		TransactionId: transactionId,
		Data:          data[:n],
	}
}

func (c *Reader) Reset() []byte {
	cache := c.cache
	c.cache = []byte{}
	return cache
}

type Writer struct {
	writer io.Writer
}

func Int2Byte(data int32) []byte {
	buf := bytes.NewBuffer([]byte{})
	err := binary.Write(buf, binary.BigEndian, data)
	if err != nil {
		log.Panic(err)
	}
	return buf.Bytes()
}

func Byte2Int(data []byte) (ret int32) {
	err := binary.Read(bytes.NewBuffer(data), binary.BigEndian, &ret)
	if err != nil {
		log.Panic(err)
	}
	return ret
}

func MarshalBody(p *Packet) []byte {
	var buf []byte
	buf = append(buf, Int2Byte(p.Cmd)...)
	buf = append(buf, Int2Byte(p.TransactionId)...)
	if p.Data != nil {
		buf = append(buf, p.Data...)
	}
	return buf[:]
}

func Marshal(packet *Packet) []byte {
	// MarshalBody
	body := MarshalBody(packet)
	// header = int32 body length
	header := Int2Byte(int32(len(body)))
	return append(header, body...)
}

func NewWriter(writer io.Writer) *Writer {
	return &Writer{writer: writer}
}

func (c *Writer) Write(packet *Packet) (err error) {
	_, err = c.writer.Write(Marshal(packet))
	return err
}

type Manager interface {

	// CreateTransaction Create And Start Transaction
	CreateTransaction(cmd int32, data []byte) Transaction

	// CreateCallbackPacket with callbackCmd……
	CreateCallbackPacket(transactionId int32, data []byte) *Packet

	IsCallbackPacket(p *Packet) bool

	// DoneTransaction Close Transaction Wait
	DoneTransaction(p *Packet) error
}

type Transaction interface {

	// GetId transactionId
	GetId() int32

	// GetPacket Get Request Packet
	GetPacket() *Packet

	// GetCallbackPacket Get Callback Packet
	GetCallbackPacket() *Packet

	// Wait Transaction has Callback Done
	Wait() error

	// ThenCallback New Goroutine to Wait and consumer callback packet
	ThenCallback(func(*Packet))
}

type Conn struct {
	net.Conn
	HandshakeHandler     Handler
	Reader               *Reader
	Writer               *Writer
	PacketHandlers       []*PacketHandler
	TransactionManager   Manager
	Attributes           map[string]interface{}
	ExceptionHandler     ExceptionHandler
	ClosedHandler        ClosedHandler
	HandshakeDoneHandler HandshakeDoneHandler
	closing              bool
	closed               bool
}

func (c *Conn) ReadPacket() (*Packet, error) {
	p, err := c.Reader.Read()
	if c.ExceptionHandler != nil {
		c.handleException(err)
		return p, err
	}
	return p, err
}

func (c *Conn) WritePacket(p *Packet) error {
	return c.Writer.Write(p)
}

func (c *Conn) Handshake() error {
	err := c.HandshakeHandler.Handshake(c)
	if err != nil {
		if c.ExceptionHandler != nil {
			c.handleException(err)
		}
	} else {
		if c.HandshakeDoneHandler != nil {
			c.HandshakeDoneHandler(c.wrapperContext())
		}
	}
	return err
}

func (c *Conn) Close() error {
	c.closing = true
	return c.SetReadDeadline(time.Now())
}

func (c *Conn) HandlePacket() {
	defer c.handleRecoverException()
	for {
		p, err := c.ReadPacket()

		if err != nil {
			panic(err)
		}
		go func() {
			for _, handler := range c.PacketHandlers {
				if p.Cmd == handler.Cmd {
					if c.Attributes == nil {
						c.Attributes = make(map[string]interface{})
					}
					err = handler.Action(c.wrapperContext(), p)
					if err != nil {
						// c.handleException(err)
						panic(err)
					}
				}
			}
		}()
	}
}

func (c *Conn) wrapperContext() *Context {
	return &Context{c}
}

func (c *Conn) handleException(err any) {
	if err == nil || c.closed {
		return
	}
	defer func() {
		c.closed = true
		_ = c.Conn.Close()
		if c.ClosedHandler != nil {
			c.ClosedHandler(c.wrapperContext())
		}
	}()
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() && c.closing {
		return
	}
	if e, ok := err.(error); ok && c.ExceptionHandler != nil {
		c.ExceptionHandler(&Context{c}, e)
	} else {
		panic(err)
	}
}

func (c *Conn) handleRecoverException() {
	c.handleException(recover())
}

type Context struct {
	*Conn
}

type PacketActionFunc func(ctx *Context, p *Packet) error

type PacketHandler struct {
	Name   string
	Cmd    int32
	Action PacketActionFunc
}

var clientMsgHandler = &PacketHandler{
	Name: "ClientMsgHandler",
	Cmd:  CmdMsg,
	Action: func(ctx *Context, packet *Packet) error {
		countInf, ok := ctx.Attributes["count"]
		var count int64
		if ok {
			count = countInf.(int64) + 1
		} else {
			count = 1
		}
		ctx.Attributes["count"] = count
		fmt.Printf("echo%d: %s\n", count, string(packet.Data))
		return nil
	},
}

type ConnWrapper func(conn net.Conn) *Conn

func Dial(network string, addr string, wrapper ConnWrapper) (*Conn, error) {
	c, err := net.Dial(network, addr)
	if err != nil {
		return nil, err
	}
	return wrapper(c), nil
}

const (
	DefaultReaderBufLength = 16
)

func NewReader(reader io.Reader) *Reader {
	return NewReaderSize(reader, DefaultReaderBufLength)
}

func NewReaderSize(reader io.Reader, bufLength int) *Reader {
	return &Reader{
		cache:     []byte{},
		reader:    reader,
		bufLength: bufLength,
	}
}

func NewPacket(cmd int32, data []byte) *Packet {
	return &Packet{
		Cmd:  cmd,
		Data: data,
	}
}

func NewPacketTransaction(cmd, transactionId int32, data []byte) *Packet {
	return &Packet{
		Cmd:           cmd,
		TransactionId: transactionId,
		Data:          data,
	}
}

type VersionClientHandler struct {
	Version float64
}

func NewVersionClientHandler(version float64) *VersionClientHandler {
	return &VersionClientHandler{
		Version: version,
	}
}

func main() {
	conn, err := Dial("tcp", ServerAddr, func(conn net.Conn) *Conn {
		return &Conn{
			Conn:             conn,
			HandshakeHandler: NewVersionClientHandler(ClientVersion),
			Reader:           NewReader(conn),
			Writer:           NewWriter(conn),
			PacketHandlers: []*PacketHandler{
				clientMsgHandler,
			},
			ExceptionHandler: func(ctx *Context, err error) {
				if err == io.EOF {
					fmt.Println("disconnect...")
					os.Exit(0)
				} else {
					panic(err)
				}
			},
		}
	})
	if err != nil {
		panic(err)
	}

	// do handshake
	_ = conn.Handshake()
	fmt.Println("Handshake Done.")

	go conn.HandlePacket()

	go notifySignalExit(conn)

	stdinReader := bufio.NewReader(os.Stdin)
	for {
		data, _, _ := stdinReader.ReadLine()
		_ = conn.WritePacket(NewPacket(CmdMsg, data))
	}
}

func notifySignalExit(conn *Conn) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT)
	go func() {
		for {
			select {
			case c := <-sigs:
				if c == syscall.SIGINT {
					// send exit msg
					fmt.Println("\nSend Exit!")
					err := conn.WritePacket(NewPacket(CmdExit, nil))
					if err != nil {
						panic(err)
					}
				}
			}
		}
	}()
}
