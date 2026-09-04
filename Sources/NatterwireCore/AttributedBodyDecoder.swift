import Foundation

public enum AttributedBodyDecoder {
    public static func decode(_ data: Data) -> String? {
        if let attributed = NSUnarchiver.unarchiveObject(with: data) as? NSAttributedString {
            return attributed.string
        }
        if let value = NSUnarchiver.unarchiveObject(with: data) as? String {
            return value
        }
        return nil
    }
}
