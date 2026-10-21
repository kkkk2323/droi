import SwiftMath
import SwiftUI
import UIKit

/// A TeX formula drawn to an image, with its depth below the baseline.
struct TypesetFormula {
    let image: UIImage
    let descent: CGFloat
}

/// Typesets TeX with SwiftMath (native, no web view). Formulas are cached by
/// source, size, colour and mode; a formula that does not parse is nil, and
/// the caller shows its source instead.
@MainActor
enum MathTypesetter {
    private static let cache = NSCache<NSString, Box>()
    private static let fonts = MTFontManager()

    private final class Box {
        let formula: TypesetFormula?
        init(_ formula: TypesetFormula?) { self.formula = formula }
    }

    static func typeset(_ tex: String, display: Bool, fontSize: CGFloat, color: UIColor) -> TypesetFormula? {
        let key = "\(display ? "D" : "T")|\(fontSize)|\(color.hashValue)|\(tex)" as NSString
        if let hit = cache.object(forKey: key) { return hit.formula }
        let formula = render(tex, display: display, fontSize: fontSize, color: color)
        cache.setObject(Box(formula), forKey: key)
        return formula
    }

    private static func render(_ tex: String, display: Bool, fontSize: CGFloat, color: UIColor) -> TypesetFormula? {
        let label = MTMathUILabel()
        label.displayErrorInline = false
        label.labelMode = display ? .display : .text
        label.textAlignment = .left
        label.font = fonts.latinModernFont(withSize: fontSize)
        label.textColor = color
        label.latex = tex
        guard label.error == nil else { return nil }
        let size = label.intrinsicContentSize
        guard size.width > 0, size.height > 0 else { return nil }
        label.frame = CGRect(origin: .zero, size: size)
        label.backgroundColor = .clear
        label.layoutIfNeeded()
        guard let line = label.displayList else { return nil }
        let format = UIGraphicsImageRendererFormat()
        format.opaque = false
        let image = UIGraphicsImageRenderer(size: size, format: format).image { context in
            // The label draws in flipped (Core Text) coordinates.
            context.cgContext.translateBy(x: 0, y: size.height)
            context.cgContext.scaleBy(x: 1, y: -1)
            label.layer.render(in: context.cgContext)
        }
        return TypesetFormula(image: image, descent: line.descent)
    }
}
