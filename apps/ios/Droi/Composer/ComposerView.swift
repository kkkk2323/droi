import DroiKit
import DroidProtocol
import PhotosUI
import SwiftUI
import UIKit

/// What the composer sends.
struct ComposerMessage {
    var text: String
    var images: [ImageAttachment]
    /// Nil sends now; `.endOfLoop` queues for the end of the run, `.endOfTurn`
    /// hands it to the running turn.
    var placement: QueuePlacement?
}

/// The message field with its attachments, slash commands, settings pills,
/// and send, queue and cancel.
struct ComposerView<Accessory: View>: View {
    @Binding var text: String
    @Binding var images: [ImageAttachment]
    var isRunning = false
    var disabled = false
    var placeholder = "Ask anything"
    var sendLabel = "Send"
    var allowEmpty = false
    var slashItems: [SlashItem] = []
    var error: String?
    var onSend: (ComposerMessage) -> Void
    var onCancel: () -> Void = {}
    @ViewBuilder var accessory: () -> Accessory

    @State private var addingImage = false
    @State private var photoItems: [PhotosPickerItem] = []
    @State private var showLibrary = false
    @State private var showCamera = false
    @State private var imageError: String?
    @FocusState private var focused: Bool
    @Environment(\.palette) private var palette
    @Environment(\.textScale) private var scale

    static var radius: CGFloat { Radius.xl + 4 }

    var body: some View {
        let picked = pickedSlashItem(text, items: slashItems)
        let query = picked == nil ? slashQuery(text, caret: text.count) : nil
        let suggestions = query.map { filterSlashItems(slashItems, query: $0) } ?? []
        VStack(alignment: .leading, spacing: Space.sm) {
            if let message = error ?? imageError {
                ThemedText(message, size: FontSize.xs, tone: .destructive, scaled: false)
                    .padding(.horizontal, Space.md)
            }
            if !suggestions.isEmpty {
                SlashMenu(items: suggestions) { item in text = "/\(item.name) " }
            }
            if !images.isEmpty {
                ScrollView(.horizontal, showsIndicators: false) {
                    HStack(spacing: Space.sm) {
                        ForEach(images) { image in AttachmentChip(image: image) { images.removeAll { $0.id == image.id } } }
                    }
                    .padding(.horizontal, Space.md)
                    .padding(.top, 6)
                }
                .accessibilityLabel("Attachments")
            }
            HStack(alignment: .top, spacing: Space.sm) {
                if let picked {
                    SlashTag(item: picked.item) { text = picked.rest }
                }
                TextField(
                    picked?.item.argumentHint ?? (isRunning ? "Queue a message" : placeholder),
                    text: Binding(
                        get: { picked?.rest ?? text },
                        set: { newValue in text = picked.map { "/\($0.item.name) " + newValue } ?? newValue }),
                    axis: .vertical
                )
                .font(.sans(FontSize.base * scale))
                .foregroundStyle(palette.foreground)
                .lineLimit(1...7)
                .focused($focused)
                .onKeyPress(.delete) {
                    // Backspace at the very start removes a slash tag.
                    if let picked, picked.rest.isEmpty {
                        text = ""
                        return .handled
                    }
                    return .ignored
                }
                .accessibilityLabel("Message")
            }
            .padding(.horizontal, Space.md)
            .frame(minHeight: 40, alignment: .center)
            if addingImage { addImageMenu }
            HStack(spacing: Space.sm) {
                IconButton(systemName: "plus", label: "Add image", size: 18) {
                    withAnimation(.snappy(duration: 0.18)) { addingImage.toggle() }
                }
                .accessibilityValue(addingImage ? "Expanded" : "Collapsed")
                ScrollView(.horizontal, showsIndicators: false) { accessory() }
                Spacer(minLength: 0)
                if isRunning {
                    roundButton("square.fill", label: "Cancel", filled: false, action: onCancel)
                    Image(systemName: "text.append")
                        .font(.system(size: 14, weight: .semibold))
                        .foregroundStyle(palette.primaryForeground)
                        .frame(width: 32, height: 32)
                        .background(palette.primary, in: Circle())
                        .contentShape(Circle())
                        .onTapGesture { send(.endOfLoop) }
                        .onLongPressGesture { send(.endOfTurn) }
                        .accessibilityElement()
                        .accessibilityLabel("Queue")
                        .accessibilityAddTraits(.isButton)
                        .accessibilityHint("Hold to hand the message to the running turn instead")
                        .accessibilityAction { send(.endOfLoop) }
                } else {
                    roundButton("arrow.up", label: sendLabel, filled: true) { send(nil) }
                        .opacity(canSend ? 1 : 0.4)
                        .disabled(!canSend)
                }
            }
            .padding(.horizontal, Space.xs)
            .padding(.bottom, Space.xs)
        }
        .padding(.top, Space.sm)
        .background(palette.background, in: RoundedRectangle(cornerRadius: Self.radius))
        .overlay(RoundedRectangle(cornerRadius: Self.radius).strokeBorder(palette.border, lineWidth: 1))
        .photosPicker(isPresented: $showLibrary, selection: $photoItems, maxSelectionCount: 5, matching: .images)
        .onChange(of: photoItems) { _, items in
            guard !items.isEmpty else { return }
            photoItems = []
            Task { await addPhotos(items) }
        }
        .fullScreenCover(isPresented: $showCamera) {
            CameraPicker { image in
                if let image, let attachment = attachment(from: image, name: "Photo \(images.count + 1)") {
                    images.append(attachment)
                }
            }
            .ignoresSafeArea()
        }
    }

    private var canSend: Bool {
        !disabled && (allowEmpty || !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !images.isEmpty)
    }

    private func send(_ placement: QueuePlacement?) {
        guard canSend || placement != nil, !disabled else { return }
        let message = ComposerMessage(text: text, images: images, placement: placement)
        guard !message.text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !message.images.isEmpty || allowEmpty
        else { return }
        text = ""
        images = []
        onSend(message)
    }

    private func roundButton(_ icon: String, label: String, filled: Bool, action: @escaping () -> Void) -> some View {
        Button(action: action) {
            Image(systemName: icon)
                .font(.system(size: 14, weight: .semibold))
                .foregroundStyle(filled ? palette.primaryForeground : palette.foreground)
                .frame(width: 32, height: 32)
                .background(filled ? palette.primary : palette.secondary, in: Circle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
    }

    private var addImageMenu: some View {
        VStack(alignment: .leading, spacing: 0) {
            menuRow("Photo library", icon: "photo.on.rectangle") { showLibrary = true }
            menuRow("Take photo", icon: "camera") {
                if UIImagePickerController.isSourceTypeAvailable(.camera) {
                    showCamera = true
                } else {
                    imageError = "This device has no camera."
                }
            }
            menuRow("Paste image", icon: "doc.on.clipboard") {
                guard let image = UIPasteboard.general.image else {
                    imageError = "There is no image on the clipboard."
                    return
                }
                if let attachment = attachment(from: image, name: "Pasted image") { images.append(attachment) }
            }
        }
        .padding(.horizontal, Space.sm)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Add image")
    }

    private func menuRow(_ title: String, icon: String, action: @escaping () -> Void) -> some View {
        Button {
            imageError = nil
            addingImage = false
            action()
        } label: {
            HStack(spacing: Space.sm) {
                Image(systemName: icon).frame(width: 20)
                Text(title).font(.sans(FontSize.sm))
                Spacer()
            }
            .foregroundStyle(palette.foreground)
            .frame(minHeight: 36)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private func addPhotos(_ items: [PhotosPickerItem]) async {
        for (offset, item) in items.enumerated() {
            guard let data = try? await item.loadTransferable(type: Data.self), let image = UIImage(data: data) else {
                continue
            }
            if let attachment = attachment(from: image, name: "Photo \(images.count + offset + 1)") {
                images.append(attachment)
            }
        }
    }
}

/// An image as the Daemon wants it: at most 1568 px on its longest edge and
/// JPEG at 0.85. The Daemon counts base64 at full length against the context
/// (chars/4), so a full-size screenshot would read as ~700k tokens.
func attachment(from image: UIImage, name: String) -> ImageAttachment? {
    let size = image.size
    let fit = fitWithin(width: Int(size.width * image.scale), height: Int(size.height * image.scale), max: maxImageEdge)
    let target = CGSize(width: fit.width, height: fit.height)
    let format = UIGraphicsImageRendererFormat()
    format.scale = 1
    let resized = UIGraphicsImageRenderer(size: target, format: format).image { _ in
        image.draw(in: CGRect(origin: .zero, size: target))
    }
    guard let jpeg = resized.jpegData(compressionQuality: imageJPEGQuality) else { return nil }
    return ImageAttachment(id: newID(), name: name, mediaType: .jpeg, data: jpeg.base64EncodedString())
}

private struct AttachmentChip: View {
    let image: ImageAttachment
    let onRemove: () -> Void
    @Environment(\.palette) private var palette

    var body: some View {
        ZStack(alignment: .topTrailing) {
            if let data = Data(base64Encoded: image.data), let ui = UIImage(data: data) {
                Image(uiImage: ui).resizable().scaledToFill()
                    .frame(width: 56, height: 56)
                    .clipShape(RoundedRectangle(cornerRadius: Radius.md))
                    .accessibilityLabel(image.name)
            }
            Button(action: onRemove) {
                Image(systemName: "xmark")
                    .font(.system(size: 9, weight: .bold))
                    .foregroundStyle(palette.primaryForeground)
                    .frame(width: 18, height: 18)
                    .background(palette.primary, in: Circle())
            }
            .buttonStyle(.plain)
            .padding(3)
            .accessibilityLabel("Remove \(image.name)")
        }
        .frame(width: 56, height: 56)
    }
}

private struct SlashMenu: View {
    let items: [SlashItem]
    let onPick: (SlashItem) -> Void
    @Environment(\.palette) private var palette

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ForEach(items) { item in
                Button {
                    onPick(item)
                } label: {
                    VStack(alignment: .leading, spacing: 1) {
                        HStack(spacing: Space.xs) {
                            Text("/\(item.name)").font(.mono(FontSize.sm, .medium)).foregroundStyle(palette.foreground)
                            if let hint = item.argumentHint {
                                Text(hint).font(.mono(FontSize.xs)).foregroundStyle(palette.mutedForeground)
                            }
                            Spacer()
                            Text(item.kind.label).font(.sans(FontSize.xs)).foregroundStyle(palette.mutedForeground)
                        }
                        if !item.description.isEmpty {
                            Text(item.description).font(.sans(FontSize.xs)).foregroundStyle(palette.mutedForeground).lineLimit(1)
                        }
                    }
                    .padding(.horizontal, Space.md)
                    .padding(.vertical, 6)
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel("Commands and skills")
    }
}

private struct SlashTag: View {
    let item: SlashItem
    let onRemove: () -> Void
    @Environment(\.palette) private var palette

    var body: some View {
        HStack(spacing: 4) {
            Image(systemName: item.kind == .skill ? "sparkles" : "slash.circle").font(.system(size: 11))
            Text(item.name).font(.sans(FontSize.sm, .medium))
            Button(action: onRemove) {
                Image(systemName: "xmark").font(.system(size: 9, weight: .bold))
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Remove \(item.kind.label.lowercased()) \(item.name)")
        }
        .foregroundStyle(palette.working)
        .padding(.horizontal, 8)
        .frame(height: 26)
        .background(palette.working.opacity(0.12), in: Capsule())
        .padding(.top, 7)
        .accessibilityElement(children: .contain)
        .accessibilityLabel("\(item.kind.label) \(item.name)")
    }
}

/// The system camera, for a photo to attach.
struct CameraPicker: UIViewControllerRepresentable {
    let onDone: (UIImage?) -> Void
    @Environment(\.dismiss) private var dismiss

    func makeCoordinator() -> Coordinator { Coordinator(self) }

    func makeUIViewController(context: Context) -> UIImagePickerController {
        let picker = UIImagePickerController()
        picker.sourceType = .camera
        picker.delegate = context.coordinator
        return picker
    }

    func updateUIViewController(_ controller: UIImagePickerController, context: Context) {}

    final class Coordinator: NSObject, UIImagePickerControllerDelegate, UINavigationControllerDelegate {
        let parent: CameraPicker
        init(_ parent: CameraPicker) { self.parent = parent }

        func imagePickerController(
            _ picker: UIImagePickerController, didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]
        ) {
            parent.onDone(info[.originalImage] as? UIImage)
            parent.dismiss()
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) {
            parent.onDone(nil)
            parent.dismiss()
        }
    }
}
