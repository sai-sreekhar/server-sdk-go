package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/pion/webrtc/v4"

	"github.com/livekit/protocol/auth"
	"github.com/livekit/protocol/logger"
	lksdk "github.com/livekit/server-sdk-go/v2"
)

var (
	host, apiKey, apiSecret, roomName, identity string
	firstAudioSubscribed                        = false
	firstVideoSubscribed                        = false
)

func init() {
	flag.StringVar(&host, "host", "", "LiveKit server host (e.g. ws://localhost:7880)")
	flag.StringVar(&apiKey, "api-key", "", "LiveKit API key")
	flag.StringVar(&apiSecret, "api-secret", "", "LiveKit API secret")
	flag.StringVar(&roomName, "room-name", "", "Room name")
	flag.StringVar(&identity, "identity", "", "Participant identity")
}

func main() {
	logger.InitFromConfig(&logger.Config{Level: "debug"}, "rtp-forward")
	lksdk.SetLogger(logger.GetLogger())
	flag.Parse()
	if host == "" || apiKey == "" || apiSecret == "" || roomName == "" || identity == "" {
		fmt.Println("invalid arguments")
		return
	}

	echoAudioTrack, err := lksdk.NewLocalTrack(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeOpus})
	if err != nil {
		panic(err)
	}
	echoVideoTrack, err := lksdk.NewLocalTrack(webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264})
	if err != nil {
		panic(err)
	}

	room := lksdk.NewRoom(&lksdk.RoomCallback{
		ParticipantCallback: lksdk.ParticipantCallback{
			OnTrackSubscribed: func(track *webrtc.TrackRemote, publication *lksdk.RemoteTrackPublication, rp *lksdk.RemoteParticipant) {
				// Forward only the first audio track
				if track.Kind() == webrtc.RTPCodecTypeAudio && !firstAudioSubscribed {
					firstAudioSubscribed = true
					go forwardRTP(track, echoAudioTrack, "audio")
				}
				// Forward only the first video track
				if track.Kind() == webrtc.RTPCodecTypeVideo && !firstVideoSubscribed {
					firstVideoSubscribed = true
					go forwardRTP(track, echoVideoTrack, "video")
				}
			},
		},
	})

	token, err := newAccessToken(apiKey, apiSecret, roomName, identity)
	if err != nil {
		panic(err)
	}

	if err := room.PrepareConnection(host, token); err != nil {
		panic(err)
	}
	if err := room.JoinWithToken(host, token); err != nil {
		panic(err)
	}

	// Publish the echo tracks.
	if _, err = room.LocalParticipant.PublishTrack(echoAudioTrack, &lksdk.TrackPublicationOptions{
		Name: "echo-audio",
	}); err != nil {
		panic(err)
	}
	if _, err = room.LocalParticipant.PublishTrack(echoVideoTrack, &lksdk.TrackPublicationOptions{
		Name: "echo-video",
	}); err != nil {
		panic(err)
	}

	// Wait for termination signal.
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	room.Disconnect()
}

func forwardRTP(track *webrtc.TrackRemote, echoTrack *lksdk.LocalTrack, kind string) {
	for {
		pkt, _, err := track.ReadRTP()
		if err != nil {
			log.Printf("%s RTP read error: %v", kind, err)
			continue
		}
		if err := echoTrack.WriteRTP(pkt, &lksdk.SampleWriteOptions{}); err != nil {
			log.Printf("%s RTP write error: %v", kind, err)
		}
	}
}

func newAccessToken(apiKey, apiSecret, roomName, pID string) (string, error) {
	at := auth.NewAccessToken(apiKey, apiSecret)
	canPub := true
	canSub := true
	grant := &auth.VideoGrant{
		RoomJoin:     true,
		Room:         roomName,
		CanPublish:   &canPub,
		CanSubscribe: &canSub,
	}
	at.SetVideoGrant(grant)
	at.SetIdentity(pID)
	at.SetName(pID)
	return at.ToJWT()
}
