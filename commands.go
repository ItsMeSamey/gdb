package gdb

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
)

// NotificationCallback is a callback used to report the notifications that GDB
// send asynchronously through the MI2 interface. Responses to the commands are
// not part of these notifications. The notification generic object contains the
// notification sent by GDB.
type NotificationCallback func(notification map[string]interface{})

// Send issues a command to GDB. Operation is the name of the MI2 command to
// execute without the leading "-" (this means that it is impossible send a CLI
// command), arguments is an optional list of arguments, in GDB parlance the can
// be: options, parameters or "--". It returns a generic object which represents
// the reply of GDB or an error in case the command cannot be delivered to GDB.
func (gdb *Gdb) Send(operation string, arguments ...string) (map[string]interface{}, error) {
	// atomically increase the sequence number
	gdb.mutex.Lock()
	sequence := strconv.FormatInt(gdb.sequence, 10)
	gdb.sequence++
	gdb.mutex.Unlock()

	// queue a pending command
	pending := make(chan map[string]interface{})
	gdb.pending[sequence] = pending

	// prepare the command
	buffer := bytes.NewBufferString(fmt.Sprintf("%s-%s", sequence, operation))
	for _, argument := range arguments {
		buffer.WriteByte(' ')
		buffer.WriteString(strconv.Quote(argument))
	}
	buffer.WriteByte('\n')

	// send the command
	if _, err := gdb.stdin.Write(buffer.Bytes()); err != nil {
		return nil, err
	}

	// wait for a response
	result := <-pending
	delete(gdb.pending, sequence)
	return result, nil
}

func (gdb *Gdb) recordReader() {
	scanner := bufio.NewScanner(gdb.stdout)
	for scanner.Scan() {
		// scan the GDB output one line at a time skipping the GDB terminator
		line := scanner.Text()
		if line == terminator {
			continue
		}

		// parse the line and distinguish between command result and
		// notification
		record := parseRecord(line)
		sequence, isResult := record[sequenceKey]
		if isResult {
			// if it is a result record remove the sequence field and complete
			// the pending command
			delete(record, sequenceKey)
			pending := gdb.pending[sequence.(string)]
			pending <- record
		} else {
			if gdb.onNotification != nil {
				gdb.onNotification(record)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		panic(err)
	}
}