package commandtree

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/oliverkofoed/dogo/snobgob"
)

type barg interface {
	Execute()
}
type streamCommand struct {
	Command CommandNode
	Threads int
}

func init() {
	snobgob.Register(streamCommand{})
}

var streamEventMarker = []byte{0, 255}

func StreamReceive(input io.Reader, output io.Writer) error {
	// read the input command
	decoder := snobgob.NewDecoder(input)
	var command streamCommand
	err := decoder.Decode(&command)
	if err != nil {
		panic("decode error:" + err.Error())
	}

	// write monitor events to output.
	buf := bytes.NewBuffer(nil)
	encoder := snobgob.NewEncoder(buf)
	r := NewRunner(command.Command, command.Threads)

	monitor := make(chan *MonitorEvent, 100000)
	go func() {
		r.Run(monitor)
		close(monitor)
	}()

	emptyLength := []byte{0, 0, 0, 0, 0, 0, 0, 0}
	lengthWriter := directWriter{}
	for evt := range monitor {
		// write streamEventMarker and empty length
		buf.Write(streamEventMarker)
		buf.Write(emptyLength)

		// serialize event to memory buffer
		err := encoder.Encode(evt)
		if err != nil {
			err = fmt.Errorf("Could not encode monitor event '%v'. Err:%v", evt, err)
			return err
		}

		arr := buf.Bytes()

		// write length of serialized event to output
		lengthWriter.arr = arr[2:10]
		err = binary.Write(lengthWriter, binary.LittleEndian, int64(len(arr)-10))
		if err != nil {
			return err
		}

		// write serialized event to output
		_, err = output.Write(arr)
		if err != nil {
			return err
		}

		// reset buffer
		buf.Reset()
	}

	return nil
}

type directWriter struct {
	arr []byte
}

func (d directWriter) Write(src []byte) (int, error) {
	if len(d.arr) != len(src) {
		panic("invalid length")
	}
	for i, b := range src {
		d.arr[i] = b
	}
	return len(src), nil
}

func StreamCall(command CommandNode, attachTo CommandNode, threads int, input io.Reader, errorReader io.Reader, output io.Writer, log func(s string)) error {
	// transfer the command to remote system
	encoder := snobgob.NewEncoder(output)
	err := encoder.Encode(&streamCommand{
		Command: command,
		Threads: threads,
	})
	if err != nil {
		return fmt.Errorf("Could not gob encode command: %v", err)
	}

	buf := bytes.NewBuffer(nil)

	decoderSource := &redirectableReader{}
	decoder := snobgob.NewDecoder(decoderSource)

	eventReader := &monitorEventReader{
		replacementRoot: attachTo.AsCommand(),
		lookup:          make(map[string]*Command),
	}
	getStdErr := func() string {
		b := bytes.NewBuffer(nil)
		io.Copy(b, errorReader)
		return b.String()
	}

	bufferArr := make([]byte, 1024*10)
	for {
		n, e := input.Read(bufferArr)
		if e != nil {
			if e == io.EOF {
				break
			}
			eventReader.finish(err)
			return err
		}
		buf.Write(bufferArr[:n])

		for {
			arr := buf.Bytes()

			// check for marker
			for i, m := range streamEventMarker {
				if i < len(arr) && arr[i] != m {
					err = fmt.Errorf("error in stream: %v, stderr: %v", buf.String(), getStdErr())
					eventReader.finish(err)
					return err
				}
			}

			if len(arr) > 10 {
				// get value length
				var byteCount int64
				err := binary.Read(bytes.NewReader(arr[2:]), binary.LittleEndian, &byteCount)
				if err != nil {
					err = fmt.Errorf("error in stream: %v. trying do decode: %v, stderr: %v", err, buf.String(), getStdErr())
					eventReader.finish(err)
					return err
				}

				if len(arr)-10 >= int(byteCount) {
					// read the event
					evt := MonitorEvent{}
					decoderSource.buf = bytes.NewBuffer(arr[10 : byteCount+10])
					err := decoder.Decode(&evt)
					if err != nil {
						err = fmt.Errorf("error in stream: %v. trying do decode: %v, %v, stderr: %v", err, buf.String(), byteCount, getStdErr())
						eventReader.finish(err)
						return err
					}

					// debug
					if evt.EventType == 99 {
						log(evt.CommandID)
					} else {
						// comment in for debugging.
						// j, _ := json.Marshal(evt)
						// log("GOT: " + string(j))
					}

					eventReader.event(&evt)

					// move buffer foward.
					buf = bytes.NewBuffer(arr[10+byteCount:])
				} else {
					break
				}
			} else {
				break
			}
		}
	}

	eventReader.finish(nil)
	return nil
}

type redirectableReader struct {
	buf *bytes.Buffer
}

func (r *redirectableReader) Read(p []byte) (n int, err error) {
	return r.buf.Read(p)
}

func (r *redirectableReader) ReadByte() (byte, error) {
	return r.buf.ReadByte()
}

type monitorEventReader struct {
	root            *Command
	replacementRoot *Command
	lookup          map[string]*Command
}

func (m *monitorEventReader) finish(err error) {
	if err == nil {
		for _, node := range m.lookup {
			if node.State != CommandStateCompleted {
				err = fmt.Errorf("One or more commands did not complete on remote system")
			}
		}
	}
	if err != nil {
		for _, node := range m.lookup {
			node.Err(err)
			node.State = CommandStateCompleted
		}
	}
}

func (m *monitorEventReader) event(evt *MonitorEvent) {
	switch evt.EventType {
	case monitorEventChildAdded:
		if _, found := m.lookup[evt.CommandID]; !found {
			node := &Command{Caption: evt.Caption, State: CommandStateReady, RemoteCommand: true}
			if evt.ParentID != "" {
				parent, parentFound := m.lookup[evt.ParentID]
				if !parentFound {
					panic("could not find parent node." + evt.ParentID)
				}
				if parent == m.root && m.replacementRoot != nil {
					parent = m.replacementRoot
				}

				parent.mutex.Lock()
				if parent.Children == nil {
					parent.Children = make([]CommandNode, 0, 5)
				}
				c := &commandWrapper{Command: *node}
				parent.Children = append(parent.Children, c)
				parent.mutex.Unlock()

				node = &c.Command
			} else {
				if m.root != nil {
					panic("already have a root node defined.")
				}
				m.root = node
			}
			m.lookup[evt.CommandID] = node
		} else {
			panic("NOT FOUND")
		}
		break
	case monitorEventStateChange:
		if node, found := m.lookup[evt.CommandID]; found {
			node.State = evt.State
		} else {
			panic("NOT FOUND")
		}
		break
	case monitorEventLog:
		if node, found := m.lookup[evt.CommandID]; found {
			node.mutex.Lock()
			if node.LogArray == nil {
				node.LogArray = make([]*LogEntry, 0, 10)
			}

			if evt.LogEntry.Error != nil {
				node.anyError = true
			}
			node.LogArray = append(node.LogArray, evt.LogEntry)
			node.mutex.Unlock()
		} else {
			panic("NOT FOUND")
		}
		break
	case monitorEventResult:
		if node, found := m.lookup[evt.CommandID]; found {
			node.result = evt.Result
		} else {
			panic("NOT FOUND")
		}
		break
	}
}

type commandWrapper struct {
	Command
}

func (w *commandWrapper) Execute() {}
