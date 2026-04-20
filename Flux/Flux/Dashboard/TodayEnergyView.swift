import FluxCore
import SwiftUI

struct TodayEnergyView: View {
    let todayEnergy: TodayEnergy?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Today")
                .font(.headline)

            energyRow(title: "Solar", value: todayEnergy?.epv)
            pairedRow(
                title: "Grid",
                positive: todayEnergy?.eInput,
                positiveLabel: "import",
                negative: todayEnergy?.eOutput,
                negativeLabel: "export"
            )
            pairedRow(
                title: "Battery",
                positive: todayEnergy?.eCharge,
                positiveLabel: "+",
                negative: todayEnergy?.eDischarge,
                negativeLabel: "-"
            )
        }
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding()
        .background(.thinMaterial, in: RoundedRectangle(cornerRadius: 16, style: .continuous))
    }

    private func energyRow(title: String, value: Double?) -> some View {
        HStack {
            Text(title)
                .foregroundStyle(.primary)
            Spacer()
            Text(formatKwh(value))
                .foregroundStyle(.primary)
        }
        .font(.subheadline)
    }

    private func pairedRow(
        title: String,
        positive: Double?,
        positiveLabel: String,
        negative: Double?,
        negativeLabel: String
    ) -> some View {
        HStack {
            Text("\(title) (\(positiveLabel)/\(negativeLabel))")
                .foregroundStyle(.primary)
            Spacer()
            Text("\(formatKwh(positive)) / \(formatKwh(negative))")
                .foregroundStyle(.primary)
        }
        .font(.subheadline)
    }

    private func formatKwh(_ value: Double?) -> String {
        guard let value else { return "—" }
        return String(format: "%.2f kWh", value)
    }
}

#if DEBUG
#Preview {
    TodayEnergyView(todayEnergy: MockFluxAPIClient.statusResponse.todayEnergy)
    .padding()
}
#endif
