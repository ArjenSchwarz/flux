#if canImport(UIKit) && !os(macOS)
import OSLog
import SwiftUI
import UIKit

struct OrientationLandscapeScope<Content: View>: View {
    @ViewBuilder var content: () -> Content

    var body: some View {
        OrientationLandscapeHost(content: content())
            .onDisappear {
                OrientationLandscapeHost<Content>.reset()
            }
    }
}

private struct OrientationLandscapeHost<Content: View>: UIViewControllerRepresentable {
    let content: Content

    func makeUIViewController(context: Context) -> Controller {
        Controller(rootView: content)
    }

    func updateUIViewController(_ controller: Controller, context: Context) {
        controller.update(rootView: content)
    }

    static func reset() {
        if OrientationLock.shared.depth > 0 {
            OrientationLock.shared.exit()
            requestPortraitGeometry()
        }
    }

    @MainActor
    static func requestPortraitGeometry() {
        guard
            let scene = UIApplication.shared.connectedScenes
                .compactMap({ $0 as? UIWindowScene })
                .first(where: { $0.activationState == .foregroundActive })
                ?? UIApplication.shared.connectedScenes
                    .compactMap({ $0 as? UIWindowScene })
                    .first
        else { return }
        scene.keyWindow?.rootViewController?.setNeedsUpdateOfSupportedInterfaceOrientations()
    }

    final class Controller: UIViewController {
        private let hosting: UIHostingController<Content>
        private var didEnter = false

        init(rootView: Content) {
            self.hosting = UIHostingController(rootView: rootView)
            super.init(nibName: nil, bundle: nil)
            addChild(hosting)
            view.addSubview(hosting.view)
            hosting.didMove(toParent: self)
            hosting.view.translatesAutoresizingMaskIntoConstraints = false
            NSLayoutConstraint.activate([
                hosting.view.topAnchor.constraint(equalTo: view.topAnchor),
                hosting.view.bottomAnchor.constraint(equalTo: view.bottomAnchor),
                hosting.view.leadingAnchor.constraint(equalTo: view.leadingAnchor),
                hosting.view.trailingAnchor.constraint(equalTo: view.trailingAnchor)
            ])
        }

        @available(*, unavailable)
        required init?(coder: NSCoder) { fatalError("init(coder:) is not supported") }

        func update(rootView: Content) {
            hosting.rootView = rootView
        }

        override func viewWillAppear(_ animated: Bool) {
            super.viewWillAppear(animated)
            guard !didEnter else { return }
            didEnter = true

            OrientationLock.shared.enter(.allButUpsideDown)
            parent?.setNeedsUpdateOfSupportedInterfaceOrientations()

            if let scene = view.window?.windowScene {
                let preferences = UIWindowScene.GeometryPreferences.iOS(interfaceOrientations: .landscape)
                scene.requestGeometryUpdate(preferences) { error in
                    Logger.expansion.info("geometry denied: \(error.localizedDescription, privacy: .public)")
                }
            }
        }

        override func viewWillDisappear(_ animated: Bool) {
            super.viewWillDisappear(animated)
            guard didEnter else { return }
            didEnter = false

            OrientationLock.shared.exit()
            parent?.setNeedsUpdateOfSupportedInterfaceOrientations()
        }
    }
}

private extension Logger {
    static let expansion = Logger(subsystem: "eu.arjen.flux", category: "chart-expansion")
}
#endif
