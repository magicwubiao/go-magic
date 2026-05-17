import { useCallback, useEffect, useState } from "react";
import { RefreshCw, Puzzle } from "lucide-react";
import { api } from "@/lib/api";
import { Button } from "@nous-research/ui";
import { Switch } from "@nous-research/ui";
import { Spinner } from "@nous-research/ui";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useToast } from "@/hooks/useToast";
import { Toast } from "@/components/Toast";
import { useI18n } from "@/i18n";
import { PluginSlot } from "@/plugins";
import { usePageHeader } from "@/contexts/usePageHeader";

export default function PluginsPage() {
  const [installId, setInstallId] = useState("");
  const [installForce, setInstallForce] = useState(false);
  const [installEnable, setInstallEnable] = useState(true);
  const [installBusy, setInstallBusy] = useState(false);
  const [rescanBusy, setRescanBusy] = useState(false);

  const { toast, showToast } = useToast();
  const { t } = useI18n();
  const { setEnd } = usePageHeader();

  useEffect(() => {
    setEnd(
      <Button
        ghost
        size="sm"
        className="shrink-0 gap-2"
        disabled={rescanBusy}
        onClick={() => void onRescan()}
      >
        {rescanBusy ? <Spinner /> : <RefreshCw className="h-3.5 w-3.5" />}
        {t.pluginsPage.refreshDashboard}
      </Button>,
    );
    return () => setEnd(null);
  }, [rescanBusy, setEnd, t.pluginsPage.refreshDashboard]);

  const onInstall = async () => {
    const id = installId.trim();
    if (!id) {
      showToast(t.pluginsPage.installHint, "error");
      return;
    }
    setInstallBusy(true);
    try {
      const r = await api.installAgentPlugin({
        identifier: id,
        force: installForce,
        enable: installEnable,
      });
      showToast(`${r.plugin_name ?? id} installed`, "success");
      if ((r.warnings?.length ?? 0) > 0) showToast(r.warnings!.join(" "), "error");
      if ((r.missing_env?.length ?? 0) > 0)
        showToast(`${t.pluginsPage.missingEnvWarn} ${r.missing_env!.join(", ")}`, "error");
      setInstallId("");
    } catch (e) {
      showToast(e instanceof Error ? e.message : "Install failed", "error");
    } finally {
      setInstallBusy(false);
    }
  };

  const onRescan = async () => {
    setRescanBusy(true);
    try {
      const rc = await api.rescanPlugins();
      showToast(
        `${t.pluginsPage.refreshDashboard} (${rc.count})`,
        "success",
      );
    } catch (e) {
      showToast(e instanceof Error ? e.message : "Rescan failed", "error");
    } finally {
      setRescanBusy(false);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      <PluginSlot name="plugins:top" />

      <div className="flex w-full flex-col gap-8">

        <Card>
          <CardHeader>
            <CardTitle>{t.pluginsPage.installHeading}</CardTitle>
            <p className="text-[0.7rem] tracking-[0.08em] text-midground/55 normal-case">
              {t.pluginsPage.installHint}
            </p>
          </CardHeader>


          <CardContent className="flex flex-col gap-4">

            <div className="flex flex-col gap-2">

              <Label htmlFor="install-url">{t.pluginsPage.identifierLabel}</Label>

              <Input
                className="normal-case font-sans lowercase"
                id="install-url"
                placeholder="owner/repo or https://..."
                spellCheck={false}
                value={installId}
                onChange={(e) => setInstallId(e.target.value)}
              />
            </div>


            <div className="flex flex-wrap items-center gap-8">

              <div className="flex items-center gap-3">

                <Switch checked={installForce} onCheckedChange={setInstallForce} />

                <span className="text-[0.7rem] tracking-[0.06em] text-midforeground/85 normal-case">
                  {t.pluginsPage.forceReinstall}
                </span>
              </div>

              <div className="flex items-center gap-3">

                <Switch checked={installEnable} onCheckedChange={setInstallEnable} />

                <span className="text-[0.7rem] tracking-[0.06em] text-midforeground/85 normal-case">
                  {t.pluginsPage.enableAfterInstall}
                </span>
              </div>
            </div>

            <Button
              className="w-fit gap-2"
              size="sm"
              disabled={installBusy}
              onClick={() => void onInstall()}
            >
              {installBusy ? <Spinner /> : <Puzzle className="h-3.5 w-3.5" />}
              {t.pluginsPage.installBtn}
            </Button>

            <p className="text-[0.65rem] tracking-[0.06em] text-midforeground/55 normal-case">
              {t.pluginsPage.rescanHint}
            </p>

            <p className="text-[0.65rem] tracking-[0.06em] text-midforeground/55 normal-case">
              {t.pluginsPage.removeHint}
            </p>
          </CardContent>
        </Card>
      </div>

      <Toast toast={toast} />
      <PluginSlot name="plugins:bottom" />
    </div>
  );
}
