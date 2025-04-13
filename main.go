package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"
	"github.com/rs/cors"
)

// Handler for incoming WHIP (WebRTC HTTP)
func whipHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid method", http.StatusMethodNotAllowed)
		return
	}

	offerData, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}

	peerConnection, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		http.Error(w, "Failed to create PeerConnection", http.StatusInternalServerError)
		return
	}

	// When a track arrives
	peerConnection.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		fmt.Printf("Received Track ID: %s, PayloadType: %d\n", track.ID(), track.PayloadType())

		// Create a file to save the received frames
		fileName := track.Kind().String() + "_" + track.ID()
		var file *os.File
		var depacketizer rtp.Depacketizer

		// Select depacketizer and file based on codec type
		switch track.Codec().MimeType {
		case webrtc.MimeTypeVP8:
			file, err = os.Create(fileName + ".vp8")
			if err != nil {
				log.Println("Failed to create file:", err)
				return
			}
			depacketizer = &codecs.VP8Packet{}
		case webrtc.MimeTypeOpus:
			file, err = os.Create(fileName + ".opus")
			if err != nil {
				log.Println("Failed to create file:", err)
				return
			}
			depacketizer = &codecs.OpusPacket{}
		default:
			log.Println("Unsupported codec:", track.Codec().MimeType)
			return
		}
		defer file.Close()

		rtpBuf := make([]byte, 1400)
		for {
			n, _, readErr := track.Read(rtpBuf)
			if readErr != nil {
				log.Println("Track read error:", readErr)
				break
			}

			packet := &rtp.Packet{}
			if err := packet.Unmarshal(rtpBuf[:n]); err != nil {
				log.Println("Failed to unmarshal RTP:", err)
				continue
			}

			// Depacketize the RTP packet to get the full frame
			payload, err := depacketizer.Unmarshal(packet.Payload)
			if err != nil {
				log.Println("Failed to depacketize RTP:", err)
				continue
			}

			// Write the frame into the file

			fmt.Println("Write.")
			_, writeErr := file.Write(payload)
			if writeErr != nil {
				log.Println("Failed to write to file:", writeErr)
				break
			}
		}
	})

	// Set remote description from the incoming SDP offer
	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  string(offerData),
	}
	if err := peerConnection.SetRemoteDescription(offer); err != nil {
		http.Error(w, "Failed to set remote description", http.StatusInternalServerError)
		return
	}

	// Create an SDP answer and set it as the local description
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		http.Error(w, "Failed to create answer", http.StatusInternalServerError)
		return
	}
	if err := peerConnection.SetLocalDescription(answer); err != nil {
		http.Error(w, "Failed to set local description", http.StatusInternalServerError)
		return
	}

	// Wait until the connection is ready
	<-webrtc.GatheringCompletePromise(peerConnection)

	// Send the SDP answer back to the client
	w.Header().Set("Content-Type", "application/sdp")
	w.WriteHeader(http.StatusCreated)
	w.Write([]byte(peerConnection.LocalDescription().SDP))

	log.Println("WHIP session established!")
}

func main() {
	// Enable CORS for all origins
	corsHandler := cors.New(cors.Options{
		AllowedOrigins: []string{"*"}, // Allow all origins (you can restrict this if needed)
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{"Content-Type", "Authorization"},
		ExposedHeaders: []string{"Content-Type"},
	})

	http.HandleFunc("/whip", whipHandler)

	// Use CORS handler properly: Pass DefaultServeMux (the default HTTP handler) to corsHandler
	handler := corsHandler.Handler(http.DefaultServeMux)

	// Start the server and use CORS middleware
	fmt.Println("Starting WHIP server on HTTP port 80...")
	err := http.ListenAndServe(":80", handler) // Apply CORS middleware
	if err != nil {
		log.Fatal(err)
	}
}
