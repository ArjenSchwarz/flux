import Foundation

/// Test double for URLProtocol used by URLSessionAPIClientPricingTests.
/// Captures the most recent request + body and dispatches to a handler.
final class PricingMockURLProtocol: URLProtocol {
    private static let lock = NSLock()
    private static var _requestHandler: (@Sendable (URLRequest) throws -> (HTTPURLResponse, Data))?
    private static var _lastRequest: URLRequest?
    private static var _lastRequestBody: Data?

    static var requestHandler: (@Sendable (URLRequest) throws -> (HTTPURLResponse, Data))? {
        get {
            lock.lock(); defer { lock.unlock() }
            return _requestHandler
        }
        set {
            lock.lock()
            _requestHandler = newValue
            _lastRequest = nil
            _lastRequestBody = nil
            lock.unlock()
        }
    }

    static var lastRequest: URLRequest? {
        get { lock.lock(); defer { lock.unlock() }; return _lastRequest }
        set { lock.lock(); _lastRequest = newValue; lock.unlock() }
    }

    static var lastRequestBody: Data? {
        lock.lock(); defer { lock.unlock() }
        return _lastRequestBody
    }

    // swiftlint:disable:next static_over_final_class
    override class func canInit(with _: URLRequest) -> Bool { true }
    // swiftlint:disable:next static_over_final_class
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        guard let handler = Self.requestHandler else {
            client?.urlProtocol(self, didFailWithError: URLError(.badServerResponse))
            return
        }
        Self.lastRequest = request
        if let stream = request.httpBodyStream {
            Self.lock.lock()
            Self._lastRequestBody = Self.readAll(from: stream)
            Self.lock.unlock()
        } else if let body = request.httpBody {
            Self.lock.lock()
            Self._lastRequestBody = body
            Self.lock.unlock()
        }
        do {
            let (response, data) = try handler(request)
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: data)
            client?.urlProtocolDidFinishLoading(self)
        } catch {
            client?.urlProtocol(self, didFailWithError: error)
        }
    }

    private static func readAll(from stream: InputStream) -> Data {
        stream.open()
        defer { stream.close() }
        var buffer = [UInt8](repeating: 0, count: 4096)
        var data = Data()
        while stream.hasBytesAvailable {
            let read = stream.read(&buffer, maxLength: buffer.count)
            if read <= 0 { break }
            data.append(buffer, count: read)
        }
        return data
    }

    override func stopLoading() {}
}
