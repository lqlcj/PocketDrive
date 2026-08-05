import { Component } from 'react';
import type { ErrorInfo, ReactNode } from 'react';
import { RotateCcw, TriangleAlert } from 'lucide-react';
import { toast } from 'sonner';
import { Button } from './ui/button';
import { copyText } from '../util';

type Props = {
    children: ReactNode;
    /** 标题里的称呼:「页面出错了」/「应用出错了」 */
    label?: string;
    /** 整个应用挂了的时候占满一屏,页面级的只占版心 */
    fullscreen?: boolean;
    /** 值一变就自动复位。传路由路径:用户点去别的页面就该恢复,而不是一直卡在错误屏 */
    resetKey?: string;
};

type State = {
    error: Error | null;
    componentStack: string;
};

/**
 * 兜住渲染期抛出的异常。
 *
 * 在这之前,任何一个组件读到 null/undefined 都会让 React 把整棵树卸掉,
 * 页面只剩一层背景色,不打开控制台根本不知道发生了什么。现在至少留一屏
 * 能看的错误信息,同时把完整日志打进 console 并支持一键复制。
 *
 * 注意边界只接渲染和生命周期里的异常——事件回调、setTimeout、Promise
 * 里抛出来的到不了这儿,那些仍然只在控制台里可见。
 */
export default class ErrorBoundary extends Component<Props, State> {
    state: State = { error: null, componentStack: '' };

    static getDerivedStateFromError(error: Error): Partial<State> {
        return { error };
    }

    componentDidCatch(error: Error, info: ErrorInfo) {
        // 生产构建里 React 自己不打日志,这行是唯一的线索来源
        console.error('[PocketDrive] 渲染出错:', error, info.componentStack);
        this.setState({ componentStack: info.componentStack ?? '' });
    }

    componentDidUpdate(prev: Props) {
        if (this.state.error && prev.resetKey !== this.props.resetKey) {
            this.reset();
        }
    }

    private reset = () => this.setState({ error: null, componentStack: '' });

    /** 攒一份能直接贴给别人的日志:环境 + 报错 + 组件栈 */
    private logText() {
        const { error, componentStack } = this.state;
        return [
            `时间: ${new Date().toISOString()}`,
            `地址: ${window.location.href}`,
            `UA:   ${navigator.userAgent}`,
            '',
            `${error?.name ?? 'Error'}: ${error?.message ?? ''}`,
            error?.stack ?? '(无调用栈)',
            '',
            '组件栈:',
            componentStack || '(无)',
        ].join('\n');
    }

    private copyLog = async () => {
        if (await copyText(this.logText())) toast.success('日志已复制');
        else toast.warning('复制失败,请手动选中日志复制');
    };

    render() {
        const { error } = this.state;
        if (!error) return this.props.children;

        const { label = '页面', fullscreen = false } = this.props;
        return (
            <div
                className={
                    fullscreen
                        ? 'min-h-screen bg-background flex items-center justify-center p-4'
                        : 'min-h-[50vh] flex items-center justify-center p-4'
                }
            >
                <div className="w-full max-w-xl bg-paper border border-line/70 rounded-[var(--radius-card)] shadow-[var(--shadow-card)] p-5">
                    <h2 className="text-lg font-extrabold flex items-center gap-2 text-danger">
                        <TriangleAlert className="size-5 shrink-0" />
                        {label}出错了
                    </h2>
                    <p className="text-sm text-ink-soft mt-1.5">
                        这一处崩了,但网盘里的文件没事。可以先重试,不行就刷新页面;
                        把下面的日志发出来能帮着定位。
                    </p>

                    <pre className="mt-3 text-xs bg-paper-2 rounded-lg p-3 max-h-28 overflow-auto whitespace-pre-wrap break-words">
                        {error.name}: {error.message}
                    </pre>

                    <details className="mt-2">
                        <summary className="text-xs text-ink-soft cursor-pointer select-none">
                            展开完整日志(调用栈 + 组件栈)
                        </summary>
                        <pre className="mt-2 text-[11px] leading-relaxed bg-paper-2 rounded-lg p-3 max-h-64 overflow-auto whitespace-pre-wrap break-words">
                            {this.logText()}
                        </pre>
                    </details>

                    <div className="flex gap-2 mt-4 flex-wrap items-center">
                        <Button variant="primary" onClick={this.reset}>
                            <RotateCcw className="size-3.5" /> 重试
                        </Button>
                        <Button onClick={() => window.location.reload()}>刷新页面</Button>
                        <Button
                            onClick={() => {
                                window.location.href = '/files';
                            }}
                        >
                            回到文件页
                        </Button>
                        <Button variant="ghost" className="ml-auto" onClick={this.copyLog}>
                            复制日志
                        </Button>
                    </div>
                </div>
            </div>
        );
    }
}
