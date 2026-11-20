// Encodes a folder of numbered JPEG frames into an H.264 MP4 at 30fps with
// macOS's AVFoundation, so no ffmpeg is needed.
//   swift encode.swift <frames dir> <out.mp4>
import AVFoundation
import CoreGraphics
import Foundation
import ImageIO

let args = CommandLine.arguments
guard args.count == 3 else { print("usage: swift encode.swift <frames dir> <out.mp4>"); exit(2) }
let dir = URL(fileURLWithPath: args[1])
let outURL = URL(fileURLWithPath: args[2])
let files = try FileManager.default.contentsOfDirectory(at: dir, includingPropertiesForKeys: nil)
  .filter { $0.pathExtension == "jpg" }.sorted { $0.lastPathComponent < $1.lastPathComponent }
guard !files.isEmpty else { print("no frames in \(dir.path)"); exit(1) }

let width = 1920, height = 1080, fps: Int32 = 30
try? FileManager.default.removeItem(at: outURL)
let writer = try AVAssetWriter(outputURL: outURL, fileType: .mp4)
let input = AVAssetWriterInput(mediaType: .video, outputSettings: [
  AVVideoCodecKey: AVVideoCodecType.h264,
  AVVideoWidthKey: width,
  AVVideoHeightKey: height,
  AVVideoCompressionPropertiesKey: [
    AVVideoAverageBitRateKey: 12_000_000,
    AVVideoProfileLevelKey: AVVideoProfileLevelH264HighAutoLevel,
    AVVideoMaxKeyFrameIntervalKey: 60,
  ],
])
input.expectsMediaDataInRealTime = false
let adaptor = AVAssetWriterInputPixelBufferAdaptor(assetWriterInput: input, sourcePixelBufferAttributes: [
  kCVPixelBufferPixelFormatTypeKey as String: kCVPixelFormatType_32ARGB,
  kCVPixelBufferWidthKey as String: width,
  kCVPixelBufferHeightKey as String: height,
])
writer.add(input)
writer.startWriting()
writer.startSession(atSourceTime: .zero)

for (i, file) in files.enumerated() {
  guard let src = CGImageSourceCreateWithURL(file as CFURL, nil),
        let image = CGImageSourceCreateImageAtIndex(src, 0, nil) else { print("cannot read \(file.path)"); exit(1) }
  while !input.isReadyForMoreMediaData { Thread.sleep(forTimeInterval: 0.005) }
  var buffer: CVPixelBuffer?
  CVPixelBufferPoolCreatePixelBuffer(nil, adaptor.pixelBufferPool!, &buffer)
  guard let px = buffer else { print("no pixel buffer"); exit(1) }
  CVPixelBufferLockBaseAddress(px, [])
  let ctx = CGContext(data: CVPixelBufferGetBaseAddress(px), width: width, height: height, bitsPerComponent: 8,
                      bytesPerRow: CVPixelBufferGetBytesPerRow(px), space: CGColorSpaceCreateDeviceRGB(),
                      bitmapInfo: CGImageAlphaInfo.noneSkipFirst.rawValue)!
  ctx.draw(image, in: CGRect(x: 0, y: 0, width: width, height: height))
  CVPixelBufferUnlockBaseAddress(px, [])
  adaptor.append(px, withPresentationTime: CMTime(value: CMTimeValue(i), timescale: fps))
}
input.markAsFinished()
let done = DispatchSemaphore(value: 0)
writer.finishWriting { done.signal() }
done.wait()
if writer.status != .completed { print("encode failed: \(writer.error?.localizedDescription ?? "unknown")"); exit(1) }
print("wrote \(outURL.path): \(files.count) frames, \(Double(files.count) / Double(fps))s")
