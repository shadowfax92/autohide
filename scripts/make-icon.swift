// Renders the autohide app icon — dark rounded square with a white
// dashed-circle "hidden face" (the 🫥 motif) — into an .iconset directory.
// Drawn with CoreGraphics rather than the emoji glyph: Apple Color Emoji is
// a bitmap font capped around 160px, so it goes blurry at 512/1024.
//
// Usage: swift scripts/make-icon.swift <out.iconset>   (then iconutil -c icns)

import CoreGraphics
import Foundation
import ImageIO

let args = CommandLine.arguments
guard args.count == 2 else {
    FileHandle.standardError.write(Data("usage: swift make-icon.swift <out.iconset>\n".utf8))
    exit(1)
}
let outDir = URL(fileURLWithPath: args[1], isDirectory: true)
try FileManager.default.createDirectory(at: outDir, withIntermediateDirectories: true)

let srgb = CGColorSpace(name: CGColorSpace.sRGB)!

func draw(_ px: Int) -> CGImage {
    let s = CGFloat(px)
    let ctx = CGContext(
        data: nil, width: px, height: px,
        bitsPerComponent: 8, bytesPerRow: 0, space: srgb,
        bitmapInfo: CGImageAlphaInfo.premultipliedLast.rawValue)!

    // Apple icon grid: content square ≈80% of canvas, the rest transparent margin.
    let inset = 0.097 * s
    let square = CGRect(x: inset, y: inset, width: s - 2 * inset, height: s - 2 * inset)
    let corner = 0.2237 * square.width
    ctx.addPath(CGPath(roundedRect: square, cornerWidth: corner, cornerHeight: corner, transform: nil))
    ctx.clip()

    let top = CGColor(colorSpace: srgb, components: [0.184, 0.208, 0.282, 1])!
    let bottom = CGColor(colorSpace: srgb, components: [0.078, 0.086, 0.118, 1])!
    let grad = CGGradient(colorsSpace: srgb, colors: [top, bottom] as CFArray, locations: [0, 1])!
    ctx.drawLinearGradient(
        grad,
        start: CGPoint(x: s / 2, y: square.maxY),
        end: CGPoint(x: s / 2, y: square.minY),
        options: [])

    let c = CGPoint(x: s / 2, y: s / 2)
    let ink = CGColor(colorSpace: srgb, components: [0.91, 0.92, 0.95, 1])!
    let lw = max(0.034 * s, 1.2)

    // Dashed face outline
    let r = 0.255 * s
    ctx.setStrokeColor(ink)
    ctx.setLineWidth(lw)
    ctx.setLineCap(.round)
    let unit = 2 * .pi * r / 20
    ctx.setLineDash(phase: 0, lengths: [unit * 0.9, unit * 1.1])
    ctx.addEllipse(in: CGRect(x: c.x - r, y: c.y - r, width: 2 * r, height: 2 * r))
    ctx.strokePath()
    ctx.setLineDash(phase: 0, lengths: [])

    // Eyes
    ctx.setFillColor(ink)
    let eyeR = max(0.031 * s, 0.9)
    for dx in [-0.10 * s, 0.10 * s] {
        ctx.fillEllipse(in: CGRect(
            x: c.x + dx - eyeR, y: c.y + 0.085 * s - eyeR,
            width: 2 * eyeR, height: 2 * eyeR))
    }

    // Mouth
    ctx.setLineWidth(lw)
    ctx.move(to: CGPoint(x: c.x - 0.085 * s, y: c.y - 0.115 * s))
    ctx.addLine(to: CGPoint(x: c.x + 0.085 * s, y: c.y - 0.115 * s))
    ctx.strokePath()

    return ctx.makeImage()!
}

var cache: [Int: CGImage] = [:]
func image(_ px: Int) -> CGImage {
    if let img = cache[px] { return img }
    let img = draw(px)
    cache[px] = img
    return img
}

func write(_ img: CGImage, to url: URL) {
    let dest = CGImageDestinationCreateWithURL(url as CFURL, "public.png" as CFString, 1, nil)!
    CGImageDestinationAddImage(dest, img, nil)
    guard CGImageDestinationFinalize(dest) else {
        FileHandle.standardError.write(Data("failed writing \(url.path)\n".utf8))
        exit(1)
    }
}

for base in [16, 32, 128, 256, 512] {
    write(image(base), to: outDir.appendingPathComponent("icon_\(base)x\(base).png"))
    write(image(base * 2), to: outDir.appendingPathComponent("icon_\(base)x\(base)@2x.png"))
}
print("iconset written to \(outDir.path)")
