import { useEffect, useRef, useState } from 'react';
import type { ModelInfo, SendImage } from '@agent-master/core';
import { useStore } from '../store.js';
import { IconImage, IconSend, IconStop, IconX } from './icons.js';

/** Shown until the daemon's live model list loads (or if it can't be fetched). */
const FALLBACK_MODELS: ModelInfo[] = [
  { id: '', label: '默认模型' },
  { id: 'opus', label: 'Opus', efforts: ['low', 'medium', 'high', 'xhigh', 'max'] },
  { id: 'sonnet', label: 'Sonnet', efforts: ['low', 'medium', 'high', 'max'] },
  { id: 'haiku', label: 'Haiku', efforts: ['low', 'medium', 'high'] },
];

/** All effort levels, with Chinese labels; filtered per model at render time. */
const EFFORT_LABELS: Record<string, string> = {
  low: '低',
  medium: '中',
  high: '高',
  xhigh: '很高',
  max: '最高',
};
const ALL_EFFORTS = ['low', 'medium', 'high', 'xhigh', 'max'];

/** An image staged in the composer before send. */
interface PendingImage {
  id: string;
  name: string;
  mediaType: string;
  data: string; // raw base64 (no data: prefix)
  url: string; // object URL for the thumbnail
}

let pendingSeq = 0;

/** Read a File into raw base64 (strips the data: prefix). */
function fileToBase64(file: File): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const result = String(reader.result);
      const comma = result.indexOf(',');
      resolve(comma >= 0 ? result.slice(comma + 1) : result);
    };
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

/**
 * Floating-card composer. Typing stays enabled during a run (drafting the next
 * instruction); only sending is gated. The send button doubles as the stop
 * button while a run is active. Supports a per-message model + effort override
 * and image attachments (paste, drag-drop, or the image button).
 */
export function Composer() {
  const sendMessage = useStore((s) => s.sendMessage);
  const interrupt = useStore((s) => s.interrupt);
  const runActive = useStore((s) => s.runActive);
  const sessionId = useStore((s) => s.currentSessionId);
  const meta = useStore((s) => s.currentSessionMeta);
  const machineId = useStore((s) => s.currentSessionMachineId);
  const fetchedModels = useStore((s) => (machineId ? s.modelsByMachine[machineId] : undefined));
  // Always have a usable list so the picker shows even against a daemon that
  // predates /api/models or when the live fetch fails.
  const models = fetchedModels && fetchedModels.length > 0 ? fetchedModels : FALLBACK_MODELS;

  const [text, setText] = useState('');
  const [sending, setSending] = useState(false);
  const [images, setImages] = useState<PendingImage[]>([]);
  const [model, setModel] = useState('');
  const [effort, setEffort] = useState('');
  const taRef = useRef<HTMLTextAreaElement>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  // Latest images, for a leak-free unmount cleanup without re-subscribing.
  const imagesRef = useRef<PendingImage[]>([]);
  imagesRef.current = images;

  // Reset the draft (text + images + picks) whenever the open session changes —
  // drafts must not carry across sessions.
  useEffect(() => {
    setText('');
    setModel('');
    setEffort('');
    if (taRef.current) taRef.current.style.height = 'auto';
    setImages((prev) => {
      prev.forEach((img) => URL.revokeObjectURL(img.url));
      return [];
    });
  }, [sessionId]);

  // Seed model/effort from the session's last-used values once its metadata
  // loads (async, after open). Keyed on meta.id so it doesn't re-run — and
  // clobber an in-progress pick — when a send updates the sticky value.
  useEffect(() => {
    if (!meta) return;
    setModel(meta.model ?? '');
    setEffort(meta.effort ?? '');
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [meta?.id]);

  // Revoke any lingering object URLs on unmount.
  useEffect(() => () => imagesRef.current.forEach((img) => URL.revokeObjectURL(img.url)), []);

  const selectedModel = models?.find((m) => m.id === model);
  // undefined (default model) → allow all; [] (known no support) → hide.
  const effortLevels = selectedModel?.efforts;
  const showEffort = effortLevels === undefined || effortLevels.length > 0;
  const effortOpts = effortLevels && effortLevels.length > 0 ? effortLevels : ALL_EFFORTS;

  const canSend = !runActive && !sending && (text.trim().length > 0 || images.length > 0);

  const autoGrow = () => {
    const ta = taRef.current;
    if (!ta) return;
    ta.style.height = 'auto';
    ta.style.height = `${Math.min(ta.scrollHeight, 200)}px`;
  };

  const addFiles = async (files: FileList | File[]) => {
    const imgs = Array.from(files).filter((f) => f.type.startsWith('image/'));
    for (const file of imgs) {
      try {
        const data = await fileToBase64(file);
        setImages((prev) => [
          ...prev,
          {
            id: `img-${pendingSeq++}`,
            name: file.name || `pasted-${pendingSeq}.png`,
            mediaType: file.type || 'image/png',
            data,
            url: URL.createObjectURL(file),
          },
        ]);
      } catch {
        // Skip an unreadable file rather than failing the whole paste.
      }
    }
  };

  const removeImage = (id: string) => {
    setImages((prev) => {
      const gone = prev.find((i) => i.id === id);
      if (gone) URL.revokeObjectURL(gone.url);
      return prev.filter((i) => i.id !== id);
    });
  };

  const onModelChange = (next: string) => {
    setModel(next);
    // Drop an effort the new model doesn't support.
    const m = models?.find((x) => x.id === next);
    if (effort && m?.efforts && !m.efforts.includes(effort)) setEffort('');
  };

  const submit = async () => {
    if (!canSend) return;
    const value = text.trim();
    const toSend: SendImage[] = images.map(({ name, mediaType, data }) => ({
      name,
      mediaType,
      data,
    }));
    setSending(true);
    setText('');
    images.forEach((img) => URL.revokeObjectURL(img.url));
    setImages([]);
    if (taRef.current) taRef.current.style.height = 'auto';
    try {
      await sendMessage(value, { model, effort, images: toSend });
    } finally {
      setSending(false);
    }
  };

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    // Enter sends; Shift+Enter inserts a newline. Enter during IME
    // composition (e.g. committing pinyin) must not send — isComposing
    // covers it, keyCode 229 catches Safari's post-compositionend timing.
    if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing && e.keyCode !== 229) {
      e.preventDefault();
      void submit();
    }
  };

  const onPaste = (e: React.ClipboardEvent<HTMLTextAreaElement>) => {
    const files = e.clipboardData?.files;
    if (files && files.length > 0 && Array.from(files).some((f) => f.type.startsWith('image/'))) {
      e.preventDefault();
      void addFiles(files);
    }
  };

  return (
    <div className="px-5 pt-1 pb-4">
      <div className="mx-auto max-w-[52rem]">
        <div
          className="rounded-2xl border border-border bg-surface shadow-sm transition-colors focus-within:border-accent"
          onDragOver={(e) => e.preventDefault()}
          onDrop={(e) => {
            if (e.dataTransfer?.files?.length) {
              e.preventDefault();
              void addFiles(e.dataTransfer.files);
            }
          }}
        >
          {images.length > 0 && (
            <div className="flex flex-wrap gap-2 px-3 pt-3">
              {images.map((img) => (
                <div key={img.id} className="group relative">
                  <img
                    src={img.url}
                    alt={img.name}
                    className="h-16 w-16 rounded-lg border border-border object-cover"
                  />
                  <button
                    onClick={() => removeImage(img.id)}
                    title="移除"
                    className="absolute -top-1.5 -right-1.5 flex h-5 w-5 items-center justify-center rounded-full border border-border bg-surface text-ink-muted shadow-sm hover:text-ink"
                  >
                    <IconX size={11} />
                  </button>
                </div>
              ))}
            </div>
          )}

          <textarea
            ref={taRef}
            value={text}
            onChange={(e) => {
              setText(e.target.value);
              autoGrow();
            }}
            onKeyDown={onKeyDown}
            onPaste={onPaste}
            placeholder="描述要交给 agent 的任务…（可粘贴/拖入图片）"
            rows={1}
            className="block max-h-50 min-h-[72px] w-full resize-none bg-transparent px-4 pt-3.5 pb-1 text-base leading-relaxed outline-none placeholder:text-ink-faint"
          />

          <div className="flex items-center gap-2 px-3 pb-2.5">
            <input
              ref={fileRef}
              type="file"
              accept="image/*"
              multiple
              className="hidden"
              onChange={(e) => {
                if (e.target.files?.length) void addFiles(e.target.files);
                e.target.value = '';
              }}
            />
            <button
              onClick={() => fileRef.current?.click()}
              title="添加图片"
              className="flex h-7 w-7 flex-none items-center justify-center rounded-md text-ink-faint hover:bg-raised hover:text-ink"
            >
              <IconImage size={15} />
            </button>

            <select
              value={model}
              onChange={(e) => onModelChange(e.target.value)}
              title="模型"
              className="max-w-[9rem] truncate rounded-md border border-border bg-surface px-1.5 py-1 text-[11px] text-ink-muted outline-none hover:text-ink focus:border-accent"
            >
              {models.map((m) => (
                <option key={m.id || 'default'} value={m.id}>
                  {m.label}
                </option>
              ))}
            </select>
            {showEffort && (
              <select
                value={effort}
                onChange={(e) => setEffort(e.target.value)}
                title="思考等级"
                className="rounded-md border border-border bg-surface px-1.5 py-1 text-[11px] text-ink-muted outline-none hover:text-ink focus:border-accent"
              >
                <option value="">思考: 默认</option>
                {effortOpts.map((lvl) => (
                  <option key={lvl} value={lvl}>
                    思考: {EFFORT_LABELS[lvl] ?? lvl}
                  </option>
                ))}
              </select>
            )}

            <span className="hidden text-[11px] text-ink-faint sm:inline">
              Enter 发送 · Shift+Enter 换行
            </span>
            <div className="flex-1" />
            {runActive ? (
              <button
                onClick={() => void interrupt()}
                title="停止运行"
                className="flex h-8 w-8 flex-none items-center justify-center rounded-full border border-danger/50 text-danger transition-colors hover:bg-danger-soft"
              >
                <IconStop size={14} />
              </button>
            ) : (
              <button
                onClick={() => void submit()}
                disabled={!canSend}
                title="发送"
                className="flex h-8 w-8 flex-none items-center justify-center rounded-full bg-accent text-on-accent transition-opacity hover:opacity-90 disabled:opacity-40"
              >
                <IconSend size={14} />
              </button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}
