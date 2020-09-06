package out

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"time"

	"github.com/glebarez/padre/pkg/color"
	"github.com/glebarez/padre/pkg/encoder"
	"github.com/glebarez/padre/pkg/printer"
)

/*
Hacky-Bar is the dynamically changing bar in status line.
The bar reflects current state of output calculation.
Apart from currently calculated part of output, it also shows yet-unknown part as a random mix of ASCII characters.
This bar is designed to be fun and fast-changing.
It also shows HTTP-client performance in real-time, such as: total http requests sent, average RPS
*/

// output refresh frequency (times/second)
const updateFreq = 13

type HackyBar struct {
	// output info
	outputData    []byte          // container for byte-output
	outputByteLen int             // total number of bytes in output (before encoding)
	encoder       encoder.Encoder // encoder for the byte-output
	Overflow      bool            // flag: terminal width overflowed, data was too wide

	// communications
	chanOutput chan byte      // delivering every byte of output via this channel
	chanReq    chan byte      // to deliver indicator of yet-another http request made
	chanStop   chan byte      // used to send a stop-signal to bar
	wg         sync.WaitGroup // used to wait for gracefull exit after stop signal sent

	// RPS calculation
	start        time.Time // the time of first request made, needed to properly calculate RPS
	requestsMade int       // total requests made, needed to calculate RPS
	rps          int       // RPS

	// the output properties
	autoUpdateFreq time.Duration // interval at which the bar must be updated
	encryptMode    bool          // whether encrypt mode is used
	maxWidth       int           // maximum width of bar in characters
	printer        *printer.Printer
}

func CreateHackyBar(encoder encoder.Encoder, outputByteLen int, encryptMode bool, maxWidth int, printer *printer.Printer) *HackyBar {
	return &HackyBar{
		outputData:     []byte{},
		outputByteLen:  outputByteLen,
		wg:             sync.WaitGroup{},
		chanOutput:     make(chan byte, 1),
		chanReq:        make(chan byte, 256),
		chanStop:       make(chan byte),
		autoUpdateFreq: time.Second / time.Duration(updateFreq),
		encoder:        encoder,
		encryptMode:    encryptMode,
		maxWidth:       maxWidth,
		printer:        printer,
	}
}

// stops the bar
func (p *HackyBar) Stop() {
	p.chanStop <- 0
	p.wg.Wait()
}

// starts the bar
func (p *HackyBar) Start() {
	go p.listenAndPrint()
}

/* designed to be run as goroutine.
collects information about current progress and then prints the info in HackyBar */
func (p *HackyBar) listenAndPrint() {
	lastPrint := time.Now() // time since last print
	stop := false           // flag: stop requested
	p.wg.Add(1)
	defer p.wg.Done()

	/* listen for incoming events */
	for {
		select {
		/* yet another output byte produced */
		case b := <-p.chanOutput:
			p.outputData = append([]byte{b}, p.outputData...) //TODO: optimize this

		/* yet another HTTP request was made. Update stats */
		case <-p.chanReq:
			if p.requestsMade == 0 {
				p.start = time.Now()
			}
			p.requestsMade++

			secsPassed := int(time.Since(p.start).Seconds())
			if secsPassed > 0 {
				p.rps = p.requestsMade / int(secsPassed)
			}

		/* stop requested */
		case <-p.chanStop:
			stop = true
		}

		// output when stop requested
		if stop {
			// avoid hacky mode
			// this is because stop can be requested when some error happened,
			// it that case we don't need to noise the unprocessed part of output with hacky string
			statusString := p.buildStatusString(false)
			p.printer.Println(statusString)
			return
		}

		// usual output
		if time.Since(lastPrint) > p.autoUpdateFreq {
			statusString := p.buildStatusString(true)
			p.printer.Println(statusString)
			lastPrint = time.Now()
		}
	}
}

/* constructs full status string to be displayed */
func (p *HackyBar) buildStatusString(hacky bool) string {
	/* the hacky-bar string is comprised of following parts |unknownOutput|knownOutput|stats|
	- unknown output is the part of output that is not yet calculated, it is represented as 'hacky' string
	- known output is the part of output that is already calculated, it is represented as output, encoded with *p.encoder
	- stats
	*/

	/* generate unknown output */
	unprocessedLen := p.outputByteLen - len(p.outputData)
	if p.encryptMode {
		unprocessedLen = len(p.encoder.EncodeToString(make([]byte, unprocessedLen)))
	}
	unknownOutput := unknownString(unprocessedLen, hacky)

	/* generate known output */
	knownOutput := p.encoder.EncodeToString(p.outputData)

	/* generate stats */
	stats := fmt.Sprintf(
		"[%d/%d] | reqs: %d (%d/sec)", len(p.outputData), p.outputByteLen, p.requestsMade, p.rps)

	/* get available space */
	availableSpace := p.maxWidth - len(stats) - 1 // -1 is for the space between output and stats
	if availableSpace < 5 {
		// a general fool-check
		panic("Your terminal is to narrow. Use a real one")
	}

	/* if we have enough space, the logic is simple */
	if availableSpace >= len(unknownOutput)+len(knownOutput) {
		output := unknownOutput + color.HiGreenBold(knownOutput)

		// pad with spaces to make stats always appear at the right edge of the screen
		output += strings.Repeat(" ", availableSpace-len(unknownOutput)-len(knownOutput))
		return fmt.Sprintf("%s %s", output, stats)
	}

	/* if we made it to here, we need to cut the output to fit into the available space
	the main idea is to choose the split-point - the poisition at which unknown output ends and known output starts */

	// at first, chose at 1/3 of available space
	splitPoint := availableSpace / 3

	// correct if knownOutput is too short yet
	if len(knownOutput) < availableSpace-splitPoint {
		splitPoint = availableSpace - len(knownOutput)
	} else if len(unknownOutput) < splitPoint {
		// correct if unknownOutput is too short
		splitPoint = len(unknownOutput)
	}

	// put ... into the end of knownOutput if it's too long
	if len(knownOutput) > availableSpace-splitPoint {
		knownOutput = knownOutput[:availableSpace-splitPoint-3] + `...`
		p.Overflow = true
	}

	outputString := unknownOutput[:splitPoint] + color.HiGreenBold(knownOutput)

	/* return the final string */
	return fmt.Sprintf("%s %s", outputString, stats)
}

/* generates string that represents the yet-unknown portion of output
when in 'hacky' mode, will produce random characters form ASCII printable range*/
func unknownString(n int, hacky bool) string {
	b := make([]byte, n)
	for i := range b {

		if hacky {
			b[i] = byte(rand.Intn(126-33) + 33) // byte from ASCII printable range
		} else {
			b[i] = '_'
		}
	}
	return string(b)
}
