package server

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

const HeaderLength int32 = 4

const (
	CmdMsg int32 = iota + 1
	CmdExit
)

type Context struct {
	*Conn
}

type ExceptionHandler func(ctx *Context, err error)
type ClosedHandler func(ctx *Context)
type HandshakeDoneHandler func(ctx *Context)

// Handler HandshakeHandler
type Handler interface {
	Handshake(conn io.ReadWriteCloser) error
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

type Manager interface {

	// CreateTransaction Create And Start Transaction
	CreateTransaction(cmd int32, data []byte) Transaction

	// CreateCallbackPacket with callbackCmd……
	CreateCallbackPacket(transactionId int32, data []byte) *Packet

	IsCallbackPacket(p *Packet) bool

	// DoneTransaction Close Transaction Wait
	DoneTransaction(p *Packet) error
}

const (
	DefaultReaderBufLength = 16
)

type Reader struct {
	cache     []byte
	reader    io.Reader
	bufLength int
}

func NewReader(reader io.Reader) *Reader {
	return NewReaderSize(reader, DefaultReaderBufLength)
}

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

func NewReaderSize(reader io.Reader, bufLength int) *Reader {
	return &Reader{
		cache:     []byte{},
		reader:    reader,
		bufLength: bufLength,
	}
}

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

func (c *Reader) Reset() []byte {
	cache := c.cache
	c.cache = []byte{}
	return cache
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

type Writer struct {
	writer io.Writer
}

func NewWriter(writer io.Writer) *Writer {
	return &Writer{writer: writer}
}

func (c *Writer) Write(packet *Packet) (err error) {
	_, err = c.writer.Write(Marshal(packet))
	return err
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

func MarshalBody(packet *Packet) []byte {
	var buf []byte
	buf = append(buf, Int2Byte(packet.Cmd)...)
	buf = append(buf, Int2Byte(packet.TransactionId)...)
	if packet.Data != nil {
		buf = append(buf, packet.Data...)
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

type ConnWrapper func(conn net.Conn) *Conn

type PacketActionFunc func(ctx *Context, p *Packet) error

type PacketHandler struct {
	Name   string
	Cmd    int32
	Action PacketActionFunc
}

var DefaultTransactionPacketAction PacketActionFunc = func(ctx *Context, p *Packet) error {
	tm := ctx.TransactionManager
	if tm == nil {
		return errors.New("transactionManager is nil")
	}
	if tm.IsCallbackPacket(p) {
		_ = tm.DoneTransaction(p)
	}
	return nil
}

const DefaultCallbackCmd = -1

var DefaultTransactionPacketHandler = &PacketHandler{
	Name:   "TransactionCallbackPacketHandler",
	Cmd:    DefaultCallbackCmd,
	Action: DefaultTransactionPacketAction,
}

var serverExitHandler = &PacketHandler{
	Name: "ServerExitHandler",
	Cmd:  CmdExit,
	Action: func(ctx *Context, p *Packet) error {
		fmt.Println("exit")
		return ctx.Close()
	},
}

type InitialFunc func(conn *Conn) interface{}

type SimpleConnStorage struct {
	ConnMap     map[*Conn]interface{}
	InitialFunc InitialFunc
}

func NewSimpleConnStorage(initialFunc InitialFunc) *SimpleConnStorage {
	return &SimpleConnStorage{
		ConnMap:     make(map[*Conn]interface{}),
		InitialFunc: initialFunc,
	}
}

func (t *SimpleConnStorage) AddConnData(conn *Conn, data interface{}) {
	if _, ok := t.ConnMap[conn]; ok {
		return
	}
	t.ConnMap[conn] = data
	fmt.Println("AddConn", len(t.ConnMap), data)
}

func (t *SimpleConnStorage) AddConn(conn *Conn) {
	var data interface{}
	if t.InitialFunc != nil {
		data = t.InitialFunc(conn)
	}
	t.AddConnData(conn, data)
}

func (t *SimpleConnStorage) RemoveConn(conn *Conn) interface{} {
	// remove from ConnMap
	data, ok := t.ConnMap[conn]
	if !ok {
		return nil
	}
	delete(t.ConnMap, conn)
	fmt.Println("RemoveConn", len(t.ConnMap), data)
	return data
}

func (t *SimpleConnStorage) DefaultHandshakeDoneHandler() HandshakeDoneHandler {
	return func(ctx *Context) {
		t.AddConn(ctx.Conn)
	}
}

func (t *SimpleConnStorage) DefaultClosedHandler() ClosedHandler {
	return func(ctx *Context) {
		t.RemoveConn(ctx.Conn)
	}
}

// Packet Network Transfer Packet
type Packet struct {
	Cmd           int32
	TransactionId int32
	Data          []byte
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

func NewServerMsgHandler(storage *SimpleConnStorage) *PacketHandler {
	return &PacketHandler{
		Name: "ServerMsgHandler",
		Cmd:  CmdMsg,
		Action: func(ctx *Context, p *Packet) error {
			fmt.Printf("receive msg: %s\n", string(p.Data))
			for conn, _ := range storage.ConnMap {
				_ = conn.WritePacket(p)
			}
			return nil
		},
	}
}

const (
	ServerVersion = 1.0
	ClientVersion = 1.0
	ServerAddr    = ":51000"
)

type VersionServerHandler struct {
	Version float64
}

func NewVersionServerHandler(version float64) *VersionServerHandler {
	return &VersionServerHandler{Version: version}
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

func NewManager() Manager {
	return NewManagerCallbackCmd(DefaultCallbackCmd)
}

type DefaultManager struct {
	CallbackCmd    int32
	transactionMap map[int32]*DefaultTransaction
	rd             *rand.Rand
	lock           sync.Mutex
}

type DefaultTransaction struct {
	Id             int32
	Packet         *Packet
	callbackPacket *Packet
	wg             *sync.WaitGroup
	// isWait value see Waiting, NotWaiting
	isWait uint32
}

func NewTransaction(id int32, cmd int32, data []byte) Transaction {
	return NewDefaultTransaction(id, cmd, data)
}

func NewDefaultTransaction(id int32, cmd int32, data []byte) *DefaultTransaction {
	p := NewPacketTransaction(cmd, id, data)
	var wg sync.WaitGroup
	wg.Add(1)
	return &DefaultTransaction{
		Id:     id,
		Packet: p,
		wg:     &wg,
	}
}

func (t *DefaultTransaction) GetId() int32 {
	return t.Id
}

func (t *DefaultTransaction) GetPacket() *Packet {
	return t.Packet
}

func (t *DefaultTransaction) GetCallbackPacket() *Packet {
	return t.callbackPacket
}

const (
	Waiting    uint32 = 1
	NotWaiting uint32 = 0
)

func (t *DefaultTransaction) Wait() error {
	if !atomic.CompareAndSwapUint32(&t.isWait, NotWaiting, Waiting) {
		return errors.New("WaitGroup is always wait")
	}
	t.isWait = Waiting
	t.wg.Wait()
	return nil
}

func (t *DefaultTransaction) ThenCallback(cb func(*Packet)) {
	go func() {
		_ = t.Wait()
		cb(t.GetCallbackPacket())
	}()
}

func (t *DefaultTransaction) done(callbackPacket *Packet) {
	t.callbackPacket = callbackPacket
	t.wg.Done()
}

func NewManagerCallbackCmd(callbackCmd int32) Manager {
	return &DefaultManager{
		CallbackCmd:    callbackCmd,
		transactionMap: make(map[int32]*DefaultTransaction),
		rd:             rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

func (m *DefaultManager) CreateTransaction(cmd int32, data []byte) Transaction {
	m.lock.Lock()
	defer m.lock.Unlock()

	id := m.generateTransactionId()
	ts := NewDefaultTransaction(id, cmd, data)
	m.transactionMap[id] = ts
	return ts
}

func (m *DefaultManager) CreateCallbackPacket(transactionId int32, data []byte) *Packet {
	return NewPacketTransaction(m.CallbackCmd, transactionId, data)
}

func (m *DefaultManager) IsCallbackPacket(p *Packet) bool {
	return m.CallbackCmd == p.Cmd
}

func (m *DefaultManager) DoneTransaction(p *Packet) error {
	if !m.IsCallbackPacket(p) {
		return errors.New("packet is not callback cmd")
	}

	m.lock.Lock()
	defer m.lock.Unlock()

	id := p.TransactionId
	ts, ok := m.transactionMap[id]
	if !ok {
		log.Printf("callback transactionId %d not found!\n", id)
		return nil
	}
	ts.done(p)
	delete(m.transactionMap, id)
	return nil
}

func (m *DefaultManager) generateTransactionId() int32 {
	for {
		id := m.rd.Int31()
		if _, ok := m.transactionMap[id]; !ok {
			return id
		}
	}
}

func Listen(network string, addr string, wrapper ConnWrapper) (*Listener, error) {
	listen, err := net.Listen(network, addr)
	if err != nil {
		return nil, err
	}
	return &Listener{
		listener:    listen,
		ConnWrapper: wrapper,
	}, nil
}

func ListenTCP(addr string, wrapper ConnWrapper) (*Listener, error) {
	return Listen("tcp", addr, wrapper)
}

type Listener struct {
	listener    net.Listener
	ConnWrapper ConnWrapper
}

func (t *Listener) Accept() (*Conn, error) {
	conn, err := t.listener.Accept()
	if err != nil {
		return nil, err
	}
	return t.ConnWrapper(conn), nil
}

func (t *Listener) Close() error {
	return t.listener.Close()
}

func (t *Listener) Addr() net.Addr {
	return t.listener.Addr()
}

func main() {

	storage := NewSimpleConnStorage(nil)

	listen, err := ListenTCP(ServerAddr, func(conn net.Conn) *Conn {
		return &Conn{
			Conn:             conn,
			HandshakeHandler: NewVersionServerHandler(ServerVersion),
			Reader:           NewReader(conn),
			Writer:           NewWriter(conn),
			PacketHandlers: []*PacketHandler{
				DefaultTransactionPacketHandler, NewServerMsgHandler(storage), serverExitHandler,
			},
			TransactionManager: NewManager(),
			ExceptionHandler: func(ctx *Context, err error) {
				panic(err)
			},
			HandshakeDoneHandler: storage.DefaultHandshakeDoneHandler(),
			ClosedHandler:        storage.DefaultClosedHandler(),
		}
	})
	if err != nil {
		panic(err)
	}
	for {
		conn, _ := listen.Accept()
		go func() {
			_ = conn.Handshake()
			conn.HandlePacket()
		}()
	}

}
