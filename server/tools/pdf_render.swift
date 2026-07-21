import Foundation
import PDFKit
import AppKit
import Vision

let args = CommandLine.arguments
guard args.count >= 3 else {
    fputs("usage: pdf_render <input.pdf> <output_dir> [max_pages]\n", stderr)
    exit(2)
}

let pdfURL = URL(fileURLWithPath: args[1])
let outDir = URL(fileURLWithPath: args[2], isDirectory: true)
let maxPagesLimit = args.count >= 4 ? (Int(args[3]) ?? 80) : 80

guard let doc = PDFDocument(url: pdfURL) else {
    fputs("cannot open pdf\n", stderr)
    exit(1)
}

do {
    try FileManager.default.createDirectory(at: outDir, withIntermediateDirectories: true)
} catch {
    fputs("cannot create output dir: \(error)\n", stderr)
    exit(1)
}

let pageCount = min(doc.pageCount, max(1, maxPagesLimit))
var textParts: [String] = []
var usedOCR = false

func ocrImage(_ image: CGImage) -> String {
    var result = ""
    let request = VNRecognizeTextRequest { request, _ in
        guard let observations = request.results as? [VNRecognizedTextObservation] else { return }
        var lines: [String] = []
        for observation in observations {
            if let candidate = observation.topCandidates(1).first {
                let t = candidate.string.trimmingCharacters(in: .whitespacesAndNewlines)
                if !t.isEmpty {
                    lines.append(t)
                }
            }
        }
        result = lines.joined(separator: "\n")
    }
    request.recognitionLevel = .accurate
    request.usesLanguageCorrection = true
    if #available(macOS 13.0, *) {
        request.recognitionLanguages = ["zh-Hans", "zh-Hant", "en-US"]
    }
    let handler = VNImageRequestHandler(cgImage: image, options: [:])
    do {
        try handler.perform([request])
    } catch {
        return ""
    }
    return result
}

for i in 0..<pageCount {
    guard let page = doc.page(at: i) else { continue }

    var pageText = page.string?.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""

    let bounds = page.bounds(for: .mediaBox)
    let scale: CGFloat = 2.0
    let width = max(1, Int((bounds.width * scale).rounded()))
    let height = max(1, Int((bounds.height * scale).rounded()))

    guard let rep = NSBitmapImageRep(
        bitmapDataPlanes: nil,
        pixelsWide: width,
        pixelsHigh: height,
        bitsPerSample: 8,
        samplesPerPixel: 4,
        hasAlpha: true,
        isPlanar: false,
        colorSpaceName: .deviceRGB,
        bytesPerRow: 0,
        bitsPerPixel: 0
    ) else {
        if !pageText.isEmpty {
            textParts.append(pageText)
        }
        continue
    }
    rep.size = NSSize(width: width, height: height)

    NSGraphicsContext.saveGraphicsState()
    if let ctx = NSGraphicsContext(bitmapImageRep: rep) {
        NSGraphicsContext.current = ctx
        let cg = ctx.cgContext
        cg.setFillColor(NSColor.white.cgColor)
        cg.fill(CGRect(x: 0, y: 0, width: width, height: height))
        cg.saveGState()
        cg.scaleBy(x: scale, y: scale)
        page.draw(with: .mediaBox, to: cg)
        cg.restoreGState()
        ctx.flushGraphics()
    }
    NSGraphicsContext.restoreGraphicsState()

    // 文本层太少时，用 Vision OCR 识别页面图片（适用于扫描件/整页图片 PDF）
    if pageText.count < 20, let cgImage = rep.cgImage {
        let ocrText = ocrImage(cgImage).trimmingCharacters(in: .whitespacesAndNewlines)
        if ocrText.count > pageText.count {
            pageText = ocrText
            usedOCR = true
        }
    }

    if !pageText.isEmpty {
        textParts.append("### 第 \(i + 1) 页\n\n\(pageText)")
    }

    guard let png = rep.representation(using: .png, properties: [:]) else { continue }
    let out = outDir.appendingPathComponent(String(format: "page-%03d.png", i + 1))
    do {
        try png.write(to: out)
    } catch {
        fputs("write page failed: \(error)\n", stderr)
    }
}

let textURL = outDir.appendingPathComponent("text.txt")
let text = textParts.joined(separator: "\n\n")
try? text.data(using: .utf8)?.write(to: textURL)
print("pages=\(pageCount)")
print("chars=\(text.count)")
print("ocr=\(usedOCR ? 1 : 0)")
