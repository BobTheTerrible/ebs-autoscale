// The buffer and bufferEvent function/struct have been adapted from https://github.com/ejholmes/cloudwatch/blob/master/writer.go

package ebs_autoscale

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatchlogs/types"
	"github.com/google/uuid"
	"io"
	"sync"
	"time"
)

type CwLogWriter struct {
	// inputChannel an internal channel consuming log messages from the Write method
	inputChannel chan []byte
	// ErrChannel exposes error messages encountered when processing logs
	ErrChannel chan error
	// LogGroupName the name of the Cloudwatch Log Group to submit log events
	LogGroupName string
	// PollInterval is the time between Cloudwatch log push events
	PollInterval uint32
	// The maximum log event batch size. Once reached, log events will be put to Cloudwatch logs ahead of the PollInterval
	MaxBatchSize uint32
}

func NewCwLogWriter(logGroupName string, pollInterval uint32, maxBatchSize uint32) *CwLogWriter {

	errChannel := make(chan error, 1)
	logChan := make(chan []byte)

	return &CwLogWriter{
		inputChannel: logChan,
		ErrChannel:   errChannel,
		LogGroupName: logGroupName,
		PollInterval: pollInterval,
		MaxBatchSize: maxBatchSize,
	}
}

// Start starts processing this writer's input channel. It will terminate when either the input channel is closed or the
// context is done.
func (c CwLogWriter) Start(ctx context.Context, client cloudwatchlogs.Client) {

	// Start the log writer, do not block.
	go c.processLogs(ctx, client)
}

// Close closes this writers input channel thus terminating any currently running processes. Writes after the writer is
// closed will cause a panic.
func (c CwLogWriter) Close() {

	close(c.inputChannel)
	close(c.ErrChannel)
}

// Write implements the io.Writer interface. Passes the bytes to the writers channel.
func (c CwLogWriter) Write(p []byte) (n int, err error) {

	c.inputChannel <- p
	return len(p), nil
}

// buffer splits up input into individual log events and inserts them into the supplied eventsBuffer
func buffer(b []byte, events *eventsBuffer) int {

	r := bufio.NewReader(bytes.NewReader(b))
	var bytesRead int

	for eof := false; !eof; {
		b, err := r.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				// flag the loop to stop
				eof = true
			} else {
				// kill the loop immediately
				break
			}
		}

		if len(b) == 0 {
			// skip to the next iteration of the loop
			continue
		}

		events.add(types.InputLogEvent{
			Message:   aws.String(string(b)),
			Timestamp: aws.Int64(time.Now().UnixNano() / int64(time.Millisecond)),
		})

		bytesRead += len(b)
	}

	return bytesRead
}

// processLogs starts a long-running process that consumes message logs from the logger.
// It will batch up log messages then submit them to a configured Cloudwatch log group.
// The process will be interrupted when the supplied context is closed.
func (c CwLogWriter) processLogs(ctx context.Context, client cloudwatchlogs.Client) {

	logStreamName := ""

	//https://elliotchance.medium.com/batch-a-channel-by-size-or-time-in-go-92fa3098f65
	for mainProcessLoop := true; mainProcessLoop; {

		// Gather logs for a period of time before sending them all to cloudwatch
		//var currentBatch []types.InputLogEvent
		currentBatch := &eventsBuffer{
			Mutex:  sync.Mutex{},
			events: []types.InputLogEvent{},
		}

		// This is resets on every loop. Once we start receiving logs it will wait this period of time before submitting
		// the logs as a batch.
		expire := time.After(time.Duration(c.PollInterval) * time.Second)

		// Wait for a period of time to gather logs. If the current batch of logs reaches the maxBatchSize then send
		// them to cloudwatch
		for gatherLoop := true; gatherLoop && mainProcessLoop; {
			select {
			case <-expire:
				// the timeout has expired. Send whatever we have to cloudwatch logs
				gatherLoop = false

			case <-ctx.Done():
				// Will kill the process if the parent context is closed.
				mainProcessLoop = false

			case logLine, ok := <-c.inputChannel:
				if !ok {
					// Will kill the process if the log writer input channel is closed
					mainProcessLoop = false
				}

				// Consume the message and add to the buffer as log-events
				buffer(logLine, currentBatch)

				if uint32(currentBatch.size()) == c.MaxBatchSize {
					// We have reached the batch size limit, time to send it off to cloudwatch logs
					gatherLoop = false
				}
			}
		}

		if currentBatch.size() > 0 {

			l, err := c.writeLogs(ctx, client, currentBatch.events, logStreamName)
			if err != nil {
				c.ErrChannel <- err
			}
			currentBatch.clear()
			// keep track of the log stream and reuse it.
			logStreamName = l
		}
	}
}

// createLogStream will make a new logStream with an uuid as its name.
func (c CwLogWriter) createLogStream(ctx context.Context, client cloudwatchlogs.Client) (string, error) {

	name := fmt.Sprintf("ebs-autoscale-%s", uuid.New().String())

	_, err := client.CreateLogStream(ctx, &cloudwatchlogs.CreateLogStreamInput{
		LogGroupName:  aws.String(c.LogGroupName),
		LogStreamName: aws.String(name),
	})

	return name, err
}

// writeLogs writes the logs to a given stream. If the logStreamName is empty it will attempt to create a new log stream
// and return the name.
func (c *CwLogWriter) writeLogs(ctx context.Context, client cloudwatchlogs.Client, logs []types.InputLogEvent, logStreamName string) (string, error) {

	var err error

	// if there is currently no log stream, create one
	if logStreamName == "" {
		logStreamName, err = c.createLogStream(ctx, client)
		if err != nil {
			return logStreamName, err
		}
	}

	// https://docs.aws.amazon.com/AmazonCloudWatchLogs/latest/APIReference/API_PutLogEvents.html
	//  The sequence token is now ignored in PutLogEvents actions. PutLogEvents actions are always accepted and never
	//  return InvalidSequenceTokenException or DataAlreadyAcceptedException even if the sequence token is not valid.
	//  You can use parallel PutLogEvents actions on the same log stream.
	_, err = client.PutLogEvents(ctx, &cloudwatchlogs.PutLogEventsInput{
		LogEvents:     logs,
		LogGroupName:  aws.String(c.LogGroupName),
		LogStreamName: aws.String(logStreamName),
	})
	if err != nil {
		return logStreamName, err
	}

	return logStreamName, nil
}

// eventsBuffer represents a locking buffer of cloudwatch events that are protected by a
// mutex.
type eventsBuffer struct {
	sync.Mutex
	events []types.InputLogEvent
}

// add a InputLogEvent to the buffer
func (b *eventsBuffer) add(event types.InputLogEvent) {

	b.Lock()
	defer b.Unlock()

	b.events = append(b.events, event)
}

// clear empties the current buffer of InputLogEvent. This should be called after they have been pushed to Cloudwatch
func (b *eventsBuffer) clear() []types.InputLogEvent {

	b.Lock()
	defer b.Unlock()
	events := b.events[:]
	b.events = nil

	return events
}

// size returns the current size of the buffer
func (b *eventsBuffer) size() int {

	b.Lock()
	defer b.Unlock()

	return len(b.events)
}
