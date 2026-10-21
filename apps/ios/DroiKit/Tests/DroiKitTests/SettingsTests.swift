import DroidProtocol
import Testing

@testable import DroiKit

private let models: JSONValue = [
    [
        "id": "auto", "displayName": "Auto Model", "modelProvider": "factory", "supportedReasoningEfforts": ["none"],
        "isCustom": false, "kind": "router",
    ],
    [
        "id": "gpt-5", "displayName": "GPT-5", "modelProvider": "openai",
        "supportedReasoningEfforts": ["low", "medium", "high"], "isCustom": false,
    ],
]

@Suite struct SessionDefaultsTests {
    @Test func readsTheDaemonsAnswerFillingWhatItLeavesOut() {
        let view = SessionDefaultsView(raw: [
            "reasoningEffort": "high",
            "availableModels": models,
            "specSavePresets": ["userFactoryDir": "/Users/dev/.factory"],
            "management": [
                "modelId": ["disabled": true, "source": "org"],
                "reasoningEffort": ["disabled": false, "source": "builtin"],
                "subagent": ["heavyModel": ["disabled": true, "source": "org"]],
            ],
        ])
        #expect(view.modelId == nil)
        #expect(view.reasoningEffort == "high")
        #expect(view.interactionMode == "auto")
        #expect(view.compactionTokenLimit == nil)
        #expect(view.compactionTokenLimitPerModel.isEmpty)
        #expect(view.compactionModel == "current-model")
        #expect(view.compactionThresholdCheckEnabled)
        #expect(view.subagentAutonomyLevel == "inherit")
        #expect(view.subagentModelSettings == SubagentModelSettings())
        #expect(view.availableAutonomyLevels == ["off", "low", "medium", "high"])
        #expect(view.userFactoryDir == "/Users/dev/.factory")
        #expect(view.models.map(\.id) == ["auto", "gpt-5"])
        #expect(view.locked == ["modelId", "subagent.heavyModel"])
    }

    @Test func readsTheTypedAnswerToo() {
        let view = SessionDefaultsView(
            GetDefaultSettingsResult(
                modelId: "gpt-5", compactionThresholdCheckEnabled: false,
                subagentModelSettings: SubagentModelSettings(lightModel: "opus")))
        #expect(view.modelId == "gpt-5")
        #expect(!view.compactionThresholdCheckEnabled)
        #expect(view.subagentModelSettings.model(for: .light) == "opus")
    }

    @Test func aPatchShowsAtOnceAndNilClearsASameAsMainChoice() {
        let view = SessionDefaultsView(raw: ["specModeModelId": "gpt-5", "compactionTokenLimit": 300_000])
        let patch = SessionDefaultsPatch(specModeModelId: .some(nil), compactionTokenLimit: 500_000)
        let next = view.applying(patch)
        #expect(next.specModeModelId == nil)
        #expect(next.compactionTokenLimit == 500_000)
        #expect(view.specModeModelId == "gpt-5")
        #expect(patch.params == ["specModeModelId": nil, "compactionTokenLimit": 500_000])
    }

    @Test func reasoningLevelsFollowTheModelOrKeepTheCurrentValue() {
        let choices = SessionDefaultsView(raw: ["availableModels": models]).models
        #expect(reasoningChoices(choices, modelId: "gpt-5", current: "high") == ["low", "medium", "high"])
        #expect(reasoningChoices(choices, modelId: "gone", current: "xhigh") == ["xhigh"])
        #expect(reasoningChoices(choices, modelId: nil, current: nil) == ["medium"])
        #expect(pickableModels(choices, routers: false).map(\.id) == ["gpt-5"])
        #expect(pickableModels(choices).map(\.id) == ["auto", "gpt-5"])
    }

    @Test func aTierChangeSendsEveryTierAndInheritingDropsTheTiersModelAndLevel() {
        let settings = SubagentModelSettings(lightModel: "gpt-5", lightReasoningEffort: .low, heavyModel: "opus")
        var withMedium = settings
        withMedium.mediumModel = "gpt-5"
        #expect(withSubagentTier(settings, .medium, .model("gpt-5")) == withMedium)
        var higher = settings
        higher.lightReasoningEffort = .high
        #expect(withSubagentTier(settings, .light, .reasoningEffort("high")) == higher)
        #expect(withSubagentTier(settings, .light, .model(nil)) == SubagentModelSettings(heavyModel: "opus"))
        #expect(
            withSubagentTier(settings, .light, .reasoningEffort(nil))
                == SubagentModelSettings(lightModel: "gpt-5", heavyModel: "opus"))
        // A new model starts from its own default level.
        #expect(
            withSubagentTier(settings, .light, .model("opus"))
                == SubagentModelSettings(lightModel: "opus", heavyModel: "opus"))
    }

    @Test func specFoldersNothingStoredIsTheHomeFolder() {
        let paths = specSavePaths(userFactoryDir: "/Users/dev/.factory")
        #expect(paths == SpecSavePaths(user: "~/.factory/docs", project: ".factory/docs"))
        #expect(specSavePaths(userFactoryDir: nil) == paths)
        #expect(specSaveChoice(nil, paths: paths) == .user)
        #expect(specSaveChoice("~/.factory/docs", paths: paths) == .user)
        #expect(specSaveChoice(".factory/docs", paths: paths) == .project)
        #expect(specSaveChoice("/Users/dev/specs", paths: paths) == .custom)
    }

    @Test func tokenLimitsReadAsFactoryWritesThem() {
        #expect(tokenLimitLabel(250_000) == "250K")
        #expect(tokenLimitLabel(1_000_000) == "1M")
        #expect(tokenLimitLabel(1_500_000) == "1.5M")
        #expect(compactionLimitChoices(current: 123_000).first == 100_000)
        #expect(compactionLimitChoices(current: 123_000).contains(123_000))
        #expect(compactionLimitChoices(current: 250_000).count == compactionLimits.count)
    }
}

private func model(_ id: String, _ provider: String?, _ label: String? = nil) -> ModelChoice {
    ModelChoice(id: id, label: label ?? id, provider: provider)
}

@Suite struct ModelChoicesTests {
    let models = [
        model("auto", nil, "Auto Model"),
        model("claude-opus-4-1", "anthropic", "Claude Opus 4.1"),
        model("gpt-5", "openai", "GPT-5"),
        model("glm-5.3-flash", "anthropic", "GLM 5.3 Flash"),
        model("gemini-3-pro", "generic-chat-completion-api", "Gemini 3 Pro"),
    ]

    @Test func readsTheBrandOffTheModelIdBeforeTrustingTheWireProvider() {
        #expect(brandOf("glm-5.3-flash", provider: "anthropic") == .zhipu)
        #expect(brandOf("gemini-3-pro", provider: "generic-chat-completion-api") == .google)
        #expect(brandOf("claude-opus-4-1", provider: "anthropic") == .anthropic)
    }

    @Test func fallsBackToTheProviderThenToOther() {
        #expect(brandOf("my-fine-tune", provider: "openai") == .openai)
        #expect(brandOf("auto", provider: nil) == .other)
        #expect(brandOf("mystery", provider: "generic-chat-completion-api") == .other)
    }

    @Test func listsTheBrandsPresentInRailOrder() {
        #expect(brandsOf(models) == [.anthropic, .openai, .google, .zhipu, .other])
    }

    @Test func showsEverythingByDefaultAndFiltersByBrand() {
        #expect(visibleModels(models, favorites: [], filter: .all, query: "").map(\.id) == models.map(\.id))
        #expect(visibleModels(models, favorites: [], filter: .brand(.zhipu), query: "").map(\.id) == ["glm-5.3-flash"])
    }

    @Test func listsFavoritesInTheOrderTheyWereStarred() {
        #expect(visibleModels(models, favorites: ["gpt-5", "auto"], filter: .favorites, query: "").map(\.id) == ["gpt-5", "auto"])
    }

    @Test func searchesLabelIdAndBrandAcrossEveryModel() {
        #expect(visibleModels(models, favorites: [], filter: .brand(.openai), query: "flash").map(\.id) == ["glm-5.3-flash"])
        #expect(visibleModels(models, favorites: [], filter: .favorites, query: "zhipu").map(\.id) == ["glm-5.3-flash"])
        #expect(visibleModels(models, favorites: [], filter: .all, query: "opus 4.1").map(\.id) == ["claude-opus-4-1"])
    }

    @Test func keepsFactorysMultiplierAndNoneForACustomModel() {
        let base: [String: JSONValue] = [
            "displayName": "m", "shortDisplayName": "m", "modelProvider": "anthropic", "supportedReasoningEfforts": [],
            "defaultReasoningEffort": "none",
        ]
        func with(_ extra: [String: JSONValue]) -> JSONValue { .object(base.merging(extra) { $1 }) }
        let choices = toModelChoices([
            with(["id": "opus", "isCustom": false, "tokenMultiplier": 1.6]),
            with(["id": "mine", "isCustom": true, "tokenMultiplier": 1]),
            with(["id": "unpriced", "isCustom": false]),
        ])
        #expect(choices.map(\.multiplier) == [1.6, nil, nil])
    }

    @Test func formatsLikeFactory() {
        #expect(formatMultiplier(1.6) == "1.6×")
        #expect(formatMultiplier(0.04) == "0.04×")
        #expect(formatMultiplier(1) == "1×")
        #expect(formatMultiplier(0.333333) == "0.33×")
        #expect(effortLabel("xhigh") == "Extra high")
        #expect(autonomyLabel("off") == "Ask for everything")
    }
}
