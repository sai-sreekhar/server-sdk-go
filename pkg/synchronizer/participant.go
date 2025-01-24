// Copyright 2023 LiveKit, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package synchronizer

import (
	"fmt"
	"sync"
	"time"

	"github.com/pion/rtcp"

	"github.com/livekit/mediatransportutil"
)

// internal struct for managing sender reports
type participantSynchronizer struct {
	sync.Mutex

	ntpStart      time.Time
	tracks        map[uint32]*TrackSynchronizer
	senderReports map[uint32]*rtcp.SenderReport
}

func (p *participantSynchronizer) onSenderReport(pkt *rtcp.SenderReport) {
	p.Lock()
	defer p.Unlock()
	fmt.Printf("pkt.ssrc: %d\n", pkt.SSRC)
	if p.ntpStart.IsZero() {
		fmt.Println("p.ntpStart is zero.")
		p.senderReports[pkt.SSRC] = pkt
		if len(p.senderReports) == len(p.tracks) {
			fmt.Println("Calling p.synchronizeTracks().")
			p.synchronizeTracks()
		}
		return
	}

	if t := p.tracks[pkt.SSRC]; t != nil {
		fmt.Printf("Calling onSenderReport function using trackSynchronizer. p.ntpStart: %v\n", p.ntpStart)
		t.onSenderReport(pkt, p.ntpStart)
	}
}

func (p *participantSynchronizer) synchronizeTracks() {
	fmt.Println("Entering participantSynchronizer.synchronizeTracks")

	// get estimated ntp start times for all tracks
	fmt.Println("Calculating estimated NTP start times for all tracks.")
	estimatedStartTimes := make(map[uint32]time.Time)

	// we will sync all tracks to the earliest
	var earliestStart time.Time
	for ssrc, pkt := range p.senderReports {
		fmt.Printf("Processing sender report: SSRC=%d, NTPTime=%v\n", ssrc, mediatransportutil.NtpTime(pkt.NTPTime).Time())

		t := p.tracks[ssrc]
		if t == nil {
			fmt.Printf("Warning: No track found for SSRC=%d\n", ssrc)
			continue
		}

		pts := t.getSenderReportPTS(pkt)
		fmt.Printf("Calculated PTS for SSRC=%d: %v\n", ssrc, pts)

		ntpStart := mediatransportutil.NtpTime(pkt.NTPTime).Time().Add(-pts)
		fmt.Printf("Calculated NTP start time for SSRC=%d: %v\n", ssrc, ntpStart)

		if earliestStart.IsZero() || ntpStart.Before(earliestStart) {
			fmt.Printf("Updating earliest start time: Previous=%v, New=%v\n", earliestStart, ntpStart)
			earliestStart = ntpStart
		}

		estimatedStartTimes[ssrc] = ntpStart
	}
	p.ntpStart = earliestStart
	fmt.Printf("Updated participant NTP start time (p.ntpStart): %v\n", p.ntpStart)

	// update pts delay so all ntp start times will match the earliest
	fmt.Println("Adjusting PTS offset for all tracks to match the earliest start time.")
	for ssrc, startedAt := range estimatedStartTimes {
		t := p.tracks[ssrc]
		if t == nil {
			fmt.Printf("Warning: No track found for SSRC=%d during PTS adjustment.\n", ssrc)
			continue
		}

		if diff := startedAt.Sub(earliestStart); diff != 0 {
			fmt.Printf("Adjusting PTS offset for SSRC=%d: CurrentPTSOffset=%v, Adjustment=%v\n", ssrc, t.ptsOffset, diff)
			t.Lock()
			t.ptsOffset += diff
			t.Unlock()
		}
	}

	fmt.Println("Exiting participantSynchronizer.synchronizeTracks")
}

func (p *participantSynchronizer) getMaxOffset() time.Duration {
	var maxOffset time.Duration

	p.Lock()
	for _, t := range p.tracks {
		t.Lock()
		if o := t.ptsOffset; o > maxOffset {
			maxOffset = o
		}
		t.Unlock()
	}
	p.Unlock()

	return maxOffset
}

func (p *participantSynchronizer) drain(maxPTS time.Duration) {
	p.Lock()
	for _, t := range p.tracks {
		t.Lock()
		t.maxPTS = maxPTS
		t.Unlock()
	}
	p.Unlock()
}
