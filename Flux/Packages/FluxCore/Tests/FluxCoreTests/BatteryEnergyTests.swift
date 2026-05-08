import Foundation
import Testing
@testable import FluxCore

@Suite
struct BatteryEnergyTests {
    @Test
    func usableKwhHappyPath() {
        // (47.2 - 5) / 100 * 13.34 = 5.629...
        let result = BatteryEnergy.usableKwh(soc: 47.2, capacityKwh: 13.34, cutoffPercent: 5)
        #expect(abs(result - 5.629) < 0.001)
    }

    @Test
    func usableKwhClampsAtZeroBelowCutoff() {
        #expect(BatteryEnergy.usableKwh(soc: 3, capacityKwh: 13.34, cutoffPercent: 5) == 0)
    }

    @Test
    func usableKwhClampsAtZeroForZeroCapacity() {
        #expect(BatteryEnergy.usableKwh(soc: 50, capacityKwh: 0, cutoffPercent: 5) == 0)
    }

    @Test
    func usableKwhClampsAtZeroForNegativeCapacity() {
        #expect(BatteryEnergy.usableKwh(soc: 50, capacityKwh: -1, cutoffPercent: 5) == 0)
    }

    @Test
    func usableKwhAtExactCutoffReturnsZero() {
        #expect(BatteryEnergy.usableKwh(soc: 5, capacityKwh: 13.34, cutoffPercent: 5) == 0)
    }
}
