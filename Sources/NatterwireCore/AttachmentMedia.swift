import Darwin
import Foundation
import ImageIO

enum AttachmentMediaError: Error {
    case notFound, tooLarge, changed
}

// File work never holds the SQLite lock. Only cache bookkeeping holds this lock.
final class AttachmentMedia: @unchecked Sendable {
    static let byteLimit = 32 * 1024 * 1024
    private let budget: Int
    private let lock = NSLock()
    private var entries: [(id: String, version: String, response: HTTPResponse)] = []
    private var bytes = 0

    init(budget: Int = 128 * 1024 * 1024) { self.budget = max(0, budget) }

    var cachedBytes: Int { lock.withLock { bytes } }

    private static func stamp(_ path: String) -> (version: String, size: Int64)? {
        var info = stat()
        guard stat(path, &info) == 0, (info.st_mode & S_IFMT) == S_IFREG else { return nil }
        return ("\(info.st_dev)-\(info.st_ino)-\(info.st_size)-\(info.st_mtimespec.tv_sec)-\(info.st_mtimespec.tv_nsec)-\(info.st_ctimespec.tv_sec)-\(info.st_ctimespec.tv_nsec)", info.st_size)
    }

    func populate(_ attachment: Attachment, metadataOnly: Bool) -> Attachment {
        var result = attachment
        result.version = "missing"
        guard let path = attachment.path, let stamp = Self.stamp(path) else { return result }
        result.version = stamp.version
        let options = [kCGImageSourceShouldCache: false] as CFDictionary
        let source = CGImageSourceCreateWithURL(URL(fileURLWithPath: path) as CFURL, options)
        if let source,
           let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, options) as? [CFString: Any],
           let width = properties[kCGImagePropertyPixelWidth] as? Int,
           let height = properties[kCGImagePropertyPixelHeight] as? Int, width > 0, height > 0 {
            let rotated = (5...8).contains(properties[kCGImagePropertyOrientation] as? Int ?? 1)
            result.width = rotated ? height : width
            result.height = rotated ? width : height
        }
        guard !metadataOnly else { return result }
        if let data = try? Self.read(path, limit: 10 * 1024 * 1024) {
            result.dataBase64 = data.base64EncodedString()
            if let source, let type = CGImageSourceGetType(source) as String?,
               ["public.heic", "public.heif"].contains(type), let id = result.mediaID {
                result.displayDataBase64 = (try? response(id: id, path: path, version: stamp.version))?.body.base64EncodedString()
            }
        }
        return result
    }

    private static func read(_ path: String, limit: Int) throws -> Data {
        guard let file = FileHandle(forReadingAtPath: path) else { throw AttachmentMediaError.notFound }
        defer { try? file.close() }
        guard let data = try? file.read(upToCount: limit + 1) else { throw AttachmentMediaError.notFound }
        guard data.count <= limit else { throw AttachmentMediaError.tooLarge }
        return data
    }

    func response(id: String, path: String, version: String?) throws -> HTTPResponse {
        guard let stamp = Self.stamp(path) else {
            invalidate(id: id)
            throw AttachmentMediaError.notFound
        }
        let cached: HTTPResponse? = lock.withLock {
            // Drop old versions even when the caller requested a stale version.
            remove { $0.id == id && $0.version != stamp.version }
            guard let index = entries.firstIndex(where: { $0.id == id && $0.version == stamp.version }) else { return nil }
            let entry = entries.remove(at: index)
            entries.append(entry)
            return entry.response
        }
        guard version == stamp.version else { throw AttachmentMediaError.changed }
        guard stamp.size <= Self.byteLimit else { throw AttachmentMediaError.tooLarge }
        if let cached {
            guard Self.stamp(path)?.version == version else { throw AttachmentMediaError.changed }
            return cached
        }
        let data = try Self.read(path, limit: Self.byteLimit)
        guard let source = CGImageSourceCreateWithData(data as CFData, [kCGImageSourceShouldCache: false] as CFDictionary),
              let type = CGImageSourceGetType(source) as String? else { throw AttachmentMediaError.notFound }
        let contentType: String
        let display: Data
        let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any]
        // Match the orientation-correct dimensions advertised in metadata,
        // including JPEG EXIF orientation which Go's image decoder ignores.
        if ["public.heic", "public.heif"].contains(type) || (properties?[kCGImagePropertyOrientation] as? Int ?? 1) != 1 {
            guard let properties = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [CFString: Any],
                  let width = properties[kCGImagePropertyPixelWidth] as? Int,
                  let height = properties[kCGImagePropertyPixelHeight] as? Int,
                  width > 0, height > 0 else { throw AttachmentMediaError.notFound }
            guard width <= 32_000_000 / height else { throw AttachmentMediaError.tooLarge }
            guard let jpeg = Attachment.displayData(data) else { throw AttachmentMediaError.notFound }
            display = jpeg
            contentType = "image/jpeg"
        } else {
            let types = ["public.jpeg": "image/jpeg", "public.png": "image/png", "com.compuserve.gif": "image/gif",
                         "org.webmproject.webp": "image/webp", "public.tiff": "image/tiff", "com.microsoft.bmp": "image/bmp"]
            guard let mime = types[type] else { throw AttachmentMediaError.notFound }
            contentType = mime
            display = data
        }
        guard display.count <= Self.byteLimit else { throw AttachmentMediaError.tooLarge }
        guard Self.stamp(path)?.version == version else { throw AttachmentMediaError.changed }
        let response = HTTPResponse(status: 200, body: display, contentType: contentType)
        lock.withLock {
            remove { $0.id == id }
            if display.count <= budget {
                while bytes > budget - display.count, !entries.isEmpty {
                    bytes -= entries.removeFirst().response.body.count
                }
                entries.append((id, stamp.version, response))
                bytes += display.count
            }
        }
        return response
    }

    private func invalidate(id: String) { lock.withLock { remove { $0.id == id } } }

    private func remove(where predicate: ((id: String, version: String, response: HTTPResponse)) -> Bool) {
        entries.removeAll { entry in
            guard predicate(entry) else { return false }
            bytes -= entry.response.body.count
            return true
        }
    }
}
