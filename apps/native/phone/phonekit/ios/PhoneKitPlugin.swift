import AVFoundation
import PhotosUI
import UIKit

/// What the Phone App asks of iOS that MyGo has no plugin for: scanning the
/// pairing code, photos picked and sent as JPEG, and the alert sounds.
final class PhoneKitPlugin: NSObject, MyGoPlugin, PHPickerViewControllerDelegate {
    private var picking: PluginReply?
    private var player: AVAudioPlayer?

    func call(_ method: String, _ args: [String: Any], _ reply: @escaping PluginReply) {
        switch method {
        case "scan":
            guard AVCaptureDevice.default(for: .video) != nil else {
                reply(.failure(PluginFailure("NoCamera", "this device has no camera")))
                return
            }
            let vc = ScannerController(hint: args["hint"] as? String ?? "", cancelTitle: args["cancel"] as? String ?? "Cancel") { code, failure in
                if let failure {
                    reply(.failure(failure))
                } else {
                    reply(.success(["code": code ?? ""]))
                }
            }
            vc.modalPresentationStyle = .fullScreen
            PluginHost.present(vc)
        case "pickImages":
            picking = reply
            var cfg = PHPickerConfiguration()
            cfg.filter = .images
            cfg.selectionLimit = 0
            let vc = PHPickerViewController(configuration: cfg)
            vc.delegate = self
            PluginHost.present(vc)
        case "play":
            guard let path = args["path"] as? String else {
                reply(.failure(PluginFailure("BadArgs", "no path")))
                return
            }
            // Ambient: the silent switch silences it, and music keeps playing under it.
            try? AVAudioSession.sharedInstance().setCategory(.ambient, options: [.mixWithOthers])
            try? AVAudioSession.sharedInstance().setActive(true)
            player = try? AVAudioPlayer(contentsOf: URL(fileURLWithPath: path))
            player?.play()
            reply(.success(nil))
        default:
            reply(.failure(PluginFailure("NoMethod", method)))
        }
    }

    func picker(_ vc: PHPickerViewController, didFinishPicking results: [PHPickerResult]) {
        vc.dismiss(animated: true)
        guard let reply = picking else { return }
        picking = nil
        let dir = NSTemporaryDirectory() + UUID().uuidString
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
        var paths = [String?](repeating: nil, count: results.count)
        let lock = NSLock()
        let group = DispatchGroup()
        for (i, r) in results.enumerated() where r.itemProvider.canLoadObject(ofClass: UIImage.self) {
            group.enter()
            r.itemProvider.loadObject(ofClass: UIImage.self) { obj, _ in
                defer { group.leave() }
                guard let image = obj as? UIImage, let data = PhoneKitPlugin.jpeg(image) else { return }
                let path = dir + "/photo-\(i + 1).jpg"
                if (try? data.write(to: URL(fileURLWithPath: path))) != nil {
                    lock.lock()
                    paths[i] = path
                    lock.unlock()
                }
            }
        }
        group.notify(queue: .main) { reply(.success(["paths": paths.compactMap { $0 }])) }
    }

    /// A photo as the model reads it best: HEIC and all as JPEG, its longest
    /// side at most 1568 pixels.
    static func jpeg(_ image: UIImage) -> Data? {
        let px = CGSize(width: image.size.width * image.scale, height: image.size.height * image.scale)
        let f = min(1, 1568 / max(px.width, px.height, 1))
        let target = CGSize(width: (px.width * f).rounded(), height: (px.height * f).rounded())
        let format = UIGraphicsImageRendererFormat()
        format.scale = 1
        let out = UIGraphicsImageRenderer(size: target, format: format).image { _ in
            image.draw(in: CGRect(origin: .zero, size: target))
        }
        return out.jpegData(compressionQuality: 0.85)
    }
}

/// The camera, full screen, until it reads a QR code or the user cancels.
final class ScannerController: UIViewController, AVCaptureMetadataOutputObjectsDelegate {
    private let session = AVCaptureSession()
    private let hint: String
    private let cancelTitle: String
    private var done: ((String?, PluginFailure?) -> Void)?

    init(hint: String, cancelTitle: String, done: @escaping (String?, PluginFailure?) -> Void) {
        self.hint = hint
        self.cancelTitle = cancelTitle
        self.done = done
        super.init(nibName: nil, bundle: nil)
    }

    required init?(coder: NSCoder) { nil }

    override func viewDidLoad() {
        super.viewDidLoad()
        view.backgroundColor = .black
        let label = UILabel()
        label.text = hint
        label.textColor = .white
        label.numberOfLines = 0
        label.textAlignment = .center
        label.font = .preferredFont(forTextStyle: .body)
        let cancel = UIButton(type: .system)
        cancel.setTitle(cancelTitle, for: .normal)
        cancel.setTitleColor(.white, for: .normal)
        cancel.titleLabel?.font = .preferredFont(forTextStyle: .headline)
        cancel.addTarget(self, action: #selector(cancelTapped), for: .touchUpInside)
        for v in [label, cancel] as [UIView] {
            v.translatesAutoresizingMaskIntoConstraints = false
            view.addSubview(v)
        }
        let g = view.safeAreaLayoutGuide
        NSLayoutConstraint.activate([
            label.leadingAnchor.constraint(equalTo: g.leadingAnchor, constant: 24),
            label.trailingAnchor.constraint(equalTo: g.trailingAnchor, constant: -24),
            label.topAnchor.constraint(equalTo: g.topAnchor, constant: 24),
            cancel.centerXAnchor.constraint(equalTo: g.centerXAnchor),
            cancel.bottomAnchor.constraint(equalTo: g.bottomAnchor, constant: -24),
        ])
        AVCaptureDevice.requestAccess(for: .video) { ok in
            DispatchQueue.main.async {
                if ok {
                    self.start()
                } else {
                    self.finish(nil, PluginFailure("Denied", "camera access was denied"))
                }
            }
        }
    }

    private func start() {
        guard let device = AVCaptureDevice.default(for: .video),
              let input = try? AVCaptureDeviceInput(device: device), session.canAddInput(input) else {
            finish(nil, PluginFailure("NoCamera", "the camera could not start"))
            return
        }
        session.addInput(input)
        let output = AVCaptureMetadataOutput()
        guard session.canAddOutput(output) else {
            finish(nil, PluginFailure("NoCamera", "the camera could not read codes"))
            return
        }
        session.addOutput(output)
        output.setMetadataObjectsDelegate(self, queue: .main)
        output.metadataObjectTypes = [.qr]
        let preview = AVCaptureVideoPreviewLayer(session: session)
        preview.frame = view.bounds
        preview.videoGravity = .resizeAspectFill
        view.layer.insertSublayer(preview, at: 0)
        let s = session
        DispatchQueue.global(qos: .userInitiated).async { s.startRunning() }
    }

    func metadataOutput(_ output: AVCaptureMetadataOutput, didOutput objects: [AVMetadataObject], from connection: AVCaptureConnection) {
        guard let code = (objects.first as? AVMetadataMachineReadableCodeObject)?.stringValue else { return }
        UINotificationFeedbackGenerator().notificationOccurred(.success)
        finish(code, nil)
    }

    @objc private func cancelTapped() { finish(nil, nil) }

    private func finish(_ code: String?, _ failure: PluginFailure?) {
        guard let done else { return }
        self.done = nil
        let s = session
        DispatchQueue.global(qos: .userInitiated).async { s.stopRunning() }
        dismiss(animated: true) { done(code, failure) }
    }
}
