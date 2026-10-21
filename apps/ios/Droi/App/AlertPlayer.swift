import AVFoundation
import DroiKit
import UIKit

/// Plays the alert sounds and haptics. The ambient audio session follows the
/// silent switch and lets other audio keep playing.
@MainActor
final class AlertPlayer {
    private var players: [AlertSound: AVAudioPlayer] = [:]
    private var configured = false

    func play(_ alert: AlertEvent, switches: AlertSwitches) {
        let feedback = feedbackFor(switches, alert)
        if let sound = feedback.sound { playSound(sound) }
        if let haptic = feedback.haptic {
            UINotificationFeedbackGenerator().notificationOccurred(haptic == .finished ? .success : .warning)
        }
    }

    private func playSound(_ sound: AlertSound) {
        if !configured {
            try? AVAudioSession.sharedInstance().setCategory(.ambient, options: [.mixWithOthers])
            configured = true
        }
        let player = players[sound] ?? {
            guard let url = Bundle.main.url(forResource: sound.rawValue, withExtension: "wav") else { return nil }
            return try? AVAudioPlayer(contentsOf: url)
        }()
        guard let player else { return }
        players[sound] = player
        player.currentTime = 0
        player.play()
    }
}
