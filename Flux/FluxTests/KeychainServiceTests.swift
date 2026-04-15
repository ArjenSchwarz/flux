import Foundation
import Testing
@testable import Flux

@MainActor @Suite(.serialized)
struct KeychainServiceTests {
    @Test
    func saveAndLoadTokenRoundTrip() throws {
        let service = makeService()
        defer { try? service.deleteToken() }

        try service.saveToken("secret-token")

        #expect(service.loadToken() == "secret-token")
    }

    @Test
    func deleteTokenRemovesStoredToken() throws {
        let service = makeService()
        defer { try? service.deleteToken() }
        try service.saveToken("secret-token")

        try service.deleteToken()

        #expect(service.loadToken() == nil)
    }

    @Test
    func loadTokenReturnsNilWhenMissing() {
        let service = makeService()
        try? service.deleteToken()

        #expect(service.loadToken() == nil)
    }

    private func makeService() -> KeychainService {
        KeychainService(
            service: "me.nore.ig.flux.tests.\(UUID().uuidString)",
            account: "api-token",
            accessGroup: nil
        )
    }
}
