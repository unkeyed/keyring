// Adds a soundtrack to the encoded video: copies the H.264 video as is and
// encodes the WAV to AAC, with macOS's AVFoundation.
//   swift mux.swift <video.mp4> <audio.wav> <out.mp4>
import AVFoundation
import Foundation

let a = CommandLine.arguments
guard a.count == 4 else { print("usage: swift mux.swift <video.mp4> <audio.wav> <out.mp4>"); exit(2) }
let videoAsset = AVURLAsset(url: URL(fileURLWithPath: a[1]))
let audioAsset = AVURLAsset(url: URL(fileURLWithPath: a[2]))
let outURL = URL(fileURLWithPath: a[3])
try? FileManager.default.removeItem(at: outURL)

func run() async throws {
  let vTrack = try await videoAsset.loadTracks(withMediaType: .video).first!
  let aTrack = try await audioAsset.loadTracks(withMediaType: .audio).first!
  let duration = try await videoAsset.load(.duration)
  let vFormat = try await vTrack.load(.formatDescriptions).first!

  let vReader = try AVAssetReader(asset: videoAsset)
  let vOut = AVAssetReaderTrackOutput(track: vTrack, outputSettings: nil)
  vReader.add(vOut)
  let aReader = try AVAssetReader(asset: audioAsset)
  aReader.timeRange = CMTimeRange(start: .zero, duration: duration)
  let aOut = AVAssetReaderTrackOutput(track: aTrack, outputSettings: [AVFormatIDKey: kAudioFormatLinearPCM])
  aReader.add(aOut)

  let writer = try AVAssetWriter(outputURL: outURL, fileType: .mp4)
  let vIn = AVAssetWriterInput(mediaType: .video, outputSettings: nil, sourceFormatHint: vFormat)
  let aIn = AVAssetWriterInput(mediaType: .audio, outputSettings: [
    AVFormatIDKey: kAudioFormatMPEG4AAC, AVSampleRateKey: 48000, AVNumberOfChannelsKey: 2, AVEncoderBitRateKey: 256_000,
  ])
  vIn.expectsMediaDataInRealTime = false
  aIn.expectsMediaDataInRealTime = false
  writer.add(vIn); writer.add(aIn)
  vReader.startReading(); aReader.startReading()
  writer.startWriting(); writer.startSession(atSourceTime: .zero)

  let group = DispatchGroup()
  for (input, output, name) in [(vIn, vOut, "video"), (aIn, aOut, "audio")] {
    group.enter()
    input.requestMediaDataWhenReady(on: DispatchQueue(label: name)) {
      while input.isReadyForMoreMediaData {
        if let sample = output.copyNextSampleBuffer() { input.append(sample) } else { input.markAsFinished(); group.leave(); return }
      }
    }
  }
  group.wait()
  await writer.finishWriting()
  if writer.status != .completed { throw writer.error ?? NSError(domain: "mux", code: 1) }
  print("wrote \(outURL.path)")
}

let done = DispatchSemaphore(value: 0)
Task { do { try await run() } catch { print("mux failed: \(error)"); exit(1) }; done.signal() }
done.wait()
