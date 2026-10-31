import Foundation

/// A documented struct.
struct Cache<Value> {
    private var entries: [String: Value] = [:]
    let limit: Int = 0x1F

    /* a block comment
       over two lines */
    func lookup(_ key: String) -> Value? {
        guard !key.isEmpty else { return nil }
        return entries[key]
    }
}
