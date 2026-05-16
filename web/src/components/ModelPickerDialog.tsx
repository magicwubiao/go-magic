import { useCallback, useEffect, useState } from "react";
import { Search, X } from "lucide-react";
import { Button } from "@nous-research/ui/ui/components/button";
import { Input } from "@/components/ui/input";
import { Spinner } from "@nous-research/ui/ui/components/spinner";
import type {
  ModelOptionsResponse,
  ModelOptionProvider,
} from "@/lib/api";

interface ModelPickerDialogProps {
  loader: () => Promise<ModelOptionsResponse>;
  title: string;
  alwaysGlobal?: boolean;
  onApply: (selection: { provider: string; model: string }) => void;
  onClose: () => void;
}

export function ModelPickerDialog({
  loader,
  title,
  alwaysGlobal = false,
  onApply,
  onClose,
}: ModelPickerDialogProps) {
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [providers, setProviders] = useState<ModelOptionProvider[]>([]);
  const [selectedProvider, setSelectedProvider] = useState<string | null>(null);
  const [selectedModel, setSelectedModel] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [applying, setApplying] = useState(false);

  useEffect(() => {
    loader()
      .then((data) => {
        setProviders(data.providers ?? []);
        if (data.provider) {
          setSelectedProvider(data.provider);
        }
        if (data.model) {
          setSelectedModel(data.model);
        }
      })
      .catch((e) => setError(String(e)))
      .finally(() => setLoading(false));
  }, [loader]);

  const handleApply = useCallback(async () => {
    if (!selectedProvider || !selectedModel) return;
    setApplying(true);
    try {
      await onApply({ provider: selectedProvider, model: selectedModel });
    } finally {
      setApplying(false);
    }
  }, [selectedProvider, selectedModel, onApply]);

  const filteredProviders = providers.filter(
    (p) =>
      p.name.toLowerCase().includes(search.toLowerCase()) ||
      p.slug.toLowerCase().includes(search.toLowerCase()),
  );

  const currentProvider = providers.find((p) => p.slug === selectedProvider);
  const models = currentProvider?.models ?? [];

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/50">
      <div className="bg-card border border-border w-full max-w-2xl max-h-[80vh] flex flex-col rounded-lg shadow-xl">
        {/* Header */}
        <div className="flex items-center justify-between px-4 py-3 border-b border-border">
          <h2 className="text-sm font-medium">{title}</h2>
          <button
            onClick={onClose}
            className="p-1 hover:bg-muted rounded"
          >
            <X className="h-4 w-4" />
          </button>
        </div>

        {/* Content */}
        <div className="flex-1 overflow-hidden flex">
          {loading ? (
            <div className="flex-1 flex items-center justify-center py-12">
              <Spinner className="text-2xl text-primary" />
            </div>
          ) : error ? (
            <div className="flex-1 flex items-center justify-center py-12">
              <p className="text-sm text-destructive">{error}</p>
            </div>
          ) : (
            <>
              {/* Provider list */}
              <div className="w-1/3 border-r border-border overflow-y-auto">
                <div className="p-2">
                  <div className="relative">
                    <Search className="absolute left-2 top-1/2 -translate-y-1/2 h-3 w-3 text-muted-foreground" />
                    <Input
                      placeholder="Search providers..."
                      value={search}
                      onChange={(e) => setSearch(e.target.value)}
                      className="pl-7 h-8 text-xs"
                    />
                  </div>
                </div>
                <div className="px-2 pb-2 space-y-0.5">
                  {filteredProviders.map((provider) => (
                    <button
                      key={provider.slug}
                      onClick={() => {
                        setSelectedProvider(provider.slug);
                        setSelectedModel(null);
                      }}
                      className={`w-full text-left px-2 py-1.5 rounded text-xs transition-colors ${
                        selectedProvider === provider.slug
                          ? "bg-primary/10 text-primary"
                          : "hover:bg-muted"
                      }`}
                    >
                      <div className="flex items-center justify-between">
                        <span className="font-medium truncate">{provider.name}</span>
                        {provider.is_current && (
                          <span className="text-[9px] text-primary">current</span>
                        )}
                      </div>
                      {provider.models && (
                        <div className="text-[10px] text-muted-foreground">
                          {provider.models.length} models
                        </div>
                      )}
                    </button>
                  ))}
                </div>
              </div>

              {/* Model list */}
              <div className="flex-1 overflow-y-auto">
                {selectedProvider ? (
                  models.length > 0 ? (
                    <div className="p-2 space-y-0.5">
                      {models.map((model) => (
                        <button
                          key={model}
                          onClick={() => setSelectedModel(model)}
                          className={`w-full text-left px-3 py-2 rounded text-xs font-mono transition-colors ${
                            selectedModel === model
                              ? "bg-primary/10 text-primary"
                              : "hover:bg-muted"
                          }`}
                        >
                          {model}
                        </button>
                      ))}
                    </div>
                  ) : (
                    <div className="flex items-center justify-center py-12 text-sm text-muted-foreground">
                      No models available
                    </div>
                  )
                ) : (
                  <div className="flex items-center justify-center py-12 text-sm text-muted-foreground">
                    Select a provider
                  </div>
                )}
              </div>
            </>
          )}
        </div>

        {/* Footer */}
        <div className="flex items-center justify-end gap-2 px-4 py-3 border-t border-border">
          <Button size="sm" outlined onClick={onClose}>
            Cancel
          </Button>
          <Button
            size="sm"
            onClick={handleApply}
            disabled={!selectedProvider || !selectedModel || applying}
            prefix={applying ? <Spinner /> : null}
          >
            Apply
          </Button>
        </div>
      </div>
    </div>
  );
}
