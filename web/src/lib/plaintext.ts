export function textDraft(text: string): string {
    return text.replace(/\r\n?/g, '\n');
}

export function textToSave(original: string, draft: string): string {
    const normalized = textDraft(draft);
    if (normalized === textDraft(original)) return original;
    const newline = original.match(/\r\n|\r|\n/)?.[0] ?? '\n';
    return normalized.replace(/\n/g, newline);
}
