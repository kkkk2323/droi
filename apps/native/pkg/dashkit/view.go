package dashkit

// Bar is a capacity bar filled to Value (0…100).
type Bar struct {
	Value float64
	Tone  Tone
}

// Badge is a short status label tinted in its Tone.
type Badge struct {
	Label string
	Tone  Tone
}

// MetricView is one figure of a strip: Value, a smaller Sub after it
// ("1.2%"), its Title, then a Note.
type MetricView struct {
	Title, Value, Sub, Note string
	Tone                    Tone
}

// Cell is a right-aligned numeric table cell; Text "—" means no value.
type Cell struct {
	Text string
	Tone Tone
	Help string
}

// Option is one choice of a filter.
type Option struct{ Value, Label string }

// ChartView is a bar a bucket against a limit, drawn as a dashed line;
// Top is the scale's top.
type ChartView struct {
	Bars       []ChartBar
	Limit      float64
	Top        float64
	LimitLabel string
	// First and Last name the first and the last bar under the axis.
	First string
	Last  string
	// Scale is one of the values in the axis's unit, which the axis counts
	// in round numbers of, and Tick writes them.
	Scale float64
	Tick  func(float64) string
	// Label is the chart's accessible name.
	Label string
}

// ChartBar is one bar: Today is drawn in the accent, Over in amber (red
// when both), and Help is what its card says.
type ChartBar struct {
	Value float64
	Today bool
	Over  bool
	Help  string
}

// TrendView is a bar a bucket, each split among Legend's parts ("Other",
// the last, drawn quiet) when Stacked. Tick writes the axis's values, Top
// is the largest bar, About describes the chart and Label names it.
type TrendView struct {
	Bars    []TrendBar
	Legend  []string
	Stacked bool
	Top     float64
	Tick    func(float64) string
	Peak    string
	About   string
	Label   string
}

// TrendBar is one bucket: its parts, from the bottom up, and what the
// card by it says. Label is its name on the axis, empty for the bars
// between those named.
type TrendBar struct {
	Parts        []float64
	Label, Title string
	Total        string
	Lines        []TrendLine
	Help         string
	Current      bool
}

// TrendLine is one part of a bar, Part its index in the legend.
type TrendLine struct {
	Part        int
	Name, Value string
}

// HeatView is a row a day by the 24 hours, each cell at one of five
// levels: 0 for none, then four that hold more. Note describes it and
// Label names it.
type HeatView struct {
	Rows  []HeatRow
	Note  string
	Label string
}

// HeatRow is one row of a HeatView, Label its name on the axis.
type HeatRow struct {
	Label string
	Cells [24]HeatCell
}

// HeatCell is one hour: its Level (0…4), what its card says, and its
// accessible Help.
type HeatCell struct {
	Level        int
	Title, Value string
	Help         string
}
