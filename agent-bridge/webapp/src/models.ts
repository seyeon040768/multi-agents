// UI catalog. IDs are persisted in agent.model.name; no API calls are made here.
export const modelCatalog: Record<string, {id: string; label: string}[]> = {
    openai: [{id: 'gpt-5.4', label: 'GPT-5.4'}],
    anthropic: [{id: 'claude-sonnet-4-6', label: 'Claude Sonnet 4.6'}],
    google: [
        {id: 'gemini-3-flash-preview', label: 'Gemini 3 Flash (Preview)'},
        {id: 'gemini-3.1-flash-lite', label: 'Gemini 3.1 Flash-Lite'},
    ],
    ollama: [{id: 'qwen3:8b', label: 'Qwen3 8B (Local)'}],
};
