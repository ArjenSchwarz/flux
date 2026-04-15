import SwiftUI

struct TodayEnergyView: View {
    let todayEnergy: TodayEnergy?

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            Text("Today")
                .font(.headline)

            energyRow(title: "Solar", value: todayEnergy?.epv)
            energyRow(title: "Grid in", value: todayEnergy?.eInput)
            energyRow(title: "Grid out", value: todayEnergy?.eOutput)
            energyRow(title: "Charged", value: todayEnergy?.eCharge)
            energyRow(title: "Discharged", value: todayEnergy?.eDischarge)
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
            Text(
                value.map { "\($0, specifier: "%.2f") kWh" } ?? "—"
            )
            .foregroundStyle(.primary)
        }
        .font(.subheadline)
    }
}

#Preview {
    TodayEnergyView(
        todayEnergy: TodayEnergy(
            epv: 14.3,
            eInput: 0.25,
            eOutput: 5.94,
            eCharge: 5.7,
            eDischarge: 6.8
        )
    )
    .padding()
}
