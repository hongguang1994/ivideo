package strm

import (
	"fmt"
	"testing"
)

func TestCleanMovieTitle(t *testing.T) {
	raws := []string{
		"特工.The.Spy.Gone.North.2018.BD1080P.X264.AAC.Korean.CHS",
		"我能说.I.Can.Speak.2017.BluRay.720p.x264.AC3",
		"我的野蛮女友.H265.1080P.国语音频.非凡科技影视小组",
		"隧道.Tunnel.2016.BD1080P.X264.AAC.Korean.CHS.BD-DY",
		"魔女.The.Witch.Part.1.The.Subversion.2018.BD1080P.韩语中字",
		"82年生的金智英.Kim.Ji-young.Born.1982.HD1080P.韩语中字",
		"七号房的礼物.Miracle.in.Cell.No.7.2013.BD1080P.X264.DTS.Mandarin&Korean.CHS",
		"共同警备区.Joint.Security.Area.2000.BD1080P.X264.DTS.Korean.CHS",
		"孤胆特工.The.Man.From.Nowhere.2010.BD1080P.X264.DTS-HD.MA.5.1.Mandarin&Korean.CHS",
		"回家的路.Way.Back.Home.2013.BluRay.1080p.x264.AAC-中文字幕",
		"开心家族-国语1080P",
		"我的野蛮女友导演剪辑版.My.Sassy.Girl.2001.DC.1080p.BluRay.H265.10bits.AAC.5.1",
		"釜山行2：半岛.Peninsula.2020.HD1080P.X264.AAC.Korean.CHS",
		"爱·回家.The.Way.Home.2002.Bluray.1080p.x264.AAC.2Audios",
	}
	for _, raw := range raws {
		title, year := CleanMovieTitle(raw)
		fmt.Printf("  [%s] (%d)\n", title, year)
	}
}
