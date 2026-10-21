import Darwin
import Foundation

/// With the launch argument `-DroiPerfLog YES`, writes the app's CPU time and
/// memory footprint once a second to Documents/perf.csv, for measuring a
/// Release build on an iPhone when Instruments cannot attach (see
/// apps/native/docs/performance.md).
enum PerfLog {
    nonisolated(unsafe) private static var timer: DispatchSourceTimer?

    static func startIfAsked() {
        guard UserDefaults.standard.bool(forKey: "DroiPerfLog"), timer == nil,
            let documents = FileManager.default.urls(for: .documentDirectory, in: .userDomainMask).first
        else { return }
        let url = documents.appendingPathComponent("perf.csv")
        FileManager.default.createFile(atPath: url.path, contents: Data("seconds,cpu_seconds,footprint_mb\n".utf8))
        guard let file = try? FileHandle(forWritingTo: url) else { return }
        file.seekToEndOfFile()
        let start = Date()
        let source = DispatchSource.makeTimerSource(queue: .global(qos: .utility))
        source.schedule(deadline: .now(), repeating: 1)
        source.setEventHandler {
            let line = String(
                format: "%.1f,%.3f,%.1f\n", Date().timeIntervalSince(start), cpuSeconds(), footprintMB())
            file.write(Data(line.utf8))
        }
        source.resume()
        timer = source
    }

    private static func cpuSeconds() -> Double {
        var usage = rusage()
        getrusage(RUSAGE_SELF, &usage)
        func seconds(_ t: timeval) -> Double { Double(t.tv_sec) + Double(t.tv_usec) / 1e6 }
        return seconds(usage.ru_utime) + seconds(usage.ru_stime)
    }

    private static func footprintMB() -> Double {
        var info = task_vm_info_data_t()
        var count = mach_msg_type_number_t(MemoryLayout<task_vm_info_data_t>.size / MemoryLayout<natural_t>.size)
        let result = withUnsafeMutablePointer(to: &info) {
            $0.withMemoryRebound(to: integer_t.self, capacity: Int(count)) {
                task_info(mach_task_self_, task_flavor_t(TASK_VM_INFO), $0, &count)
            }
        }
        return result == KERN_SUCCESS ? Double(info.phys_footprint) / 1_048_576 : 0
    }
}
